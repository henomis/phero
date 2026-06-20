// Copyright 2026 Simone Vellei
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package natsmemory

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
)

var _ memory.Memory = (*Memory)(nil)

// Option configures a Memory instance.
type Option func(*Memory)

// Memory stores llm.Message values in a NATS JetStream Key-Value bucket,
// scoped to a single session.
//
// The provided nats.KeyValue is treated as an injected dependency and is not
// owned by Memory (i.e. Memory does not close the underlying NATS connection).
type Memory struct {
	kv        nats.KeyValue
	sessionID string
	mu        sync.Mutex

	llm              llm.LLM
	summaryThreshold uint
	summarySize      uint
}

// New creates a new NATS JetStream KV-backed memory bound to sessionID.
func New(kv nats.KeyValue, sessionID string, options ...Option) (*Memory, error) {
	if kv == nil {
		return nil, ErrNilKeyValue
	}

	if strings.TrimSpace(sessionID) == "" {
		return nil, ErrEmptySessionID
	}

	m := &Memory{
		kv:        kv,
		sessionID: sessionID,
	}

	for _, opt := range options {
		if opt != nil {
			opt(m)
		}
	}

	return m, nil
}

// WithSummarization enables automatic summarization when the number of stored
// messages exceeds summarizeThreshold.
func WithSummarization(summaryLLM llm.LLM, summarizeThreshold, summarySize uint) Option {
	return func(m *Memory) {
		m.llm = summaryLLM
		m.summarySize = memory.ClampSummarySize(summarizeThreshold, summarySize)
		m.summaryThreshold = summarizeThreshold
	}
}

func (m *Memory) needSummarization(msgCount int) bool {
	return m.llm != nil && m.summaryThreshold > 0 && msgCount >= int(m.summaryThreshold)
}

// load retrieves and JSON-decodes the current message list from the KV store.
// Returns an empty slice when the key does not exist yet.
// The caller must hold m.mu.
func (m *Memory) load() ([]llm.Message, error) {
	entry, err := m.kv.Get(m.sessionID)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return []llm.Message{}, nil
		}

		return nil, err
	}

	var msgs []llm.Message
	if unmarshalErr := json.Unmarshal(entry.Value(), &msgs); unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return msgs, nil
}

// store JSON-encodes and writes the message list back to the KV store.
// The caller must hold m.mu.
func (m *Memory) store(msgs []llm.Message) error {
	data, err := json.Marshal(msgs)
	if err != nil {
		return err
	}

	_, err = m.kv.Put(m.sessionID, data)

	return err
}

// Save appends messages to the session history.
func (m *Memory) Save(ctx context.Context, messages []llm.Message) error {
	if len(messages) == 0 {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, err := m.load()
	if err != nil {
		return err
	}

	merged := append(existing, messages...)

	if m.needSummarization(len(merged)) {
		toSummarize := merged[:m.summarySize]
		toAppend := merged[m.summarySize:]

		history := memory.FormatSummaryPrompt(toSummarize)

		summaryMsg, llmErr := m.llm.Execute(ctx, []llm.Message{history}, nil)
		if llmErr != nil {
			return llmErr
		}

		merged = []llm.Message{
			llm.SystemMessage(memory.SummarySystemMessagePrefix + summaryMsg.Message.TextContent()),
		}
		merged = append(merged, toAppend...)
	}

	return m.store(merged)
}

// Retrieve returns all messages currently in memory, ordered from oldest to newest.
//
// query is ignored (matching the behaviour of memory/simple and memory/psql).
func (m *Memory) Retrieve(_ context.Context, _ string) ([]llm.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.load()
}

// Clear removes all messages for this session.
func (m *Memory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.kv.Purge(m.sessionID); err != nil && err != nats.ErrKeyNotFound {
		return err
	}

	return nil
}
