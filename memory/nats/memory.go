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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/v2/internal/natskv"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
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

// Open binds the JetStream key-value bucket named bucket on nc — creating it if
// it does not exist — and returns a Memory over it, scoped to sessionID.
//
// It exists because [New] takes an already-provisioned bucket, leaving every
// caller to write the same create-then-bind dance: CreateKeyValue reports
// ErrStreamNameAlreadyInUse for a bucket that is already there, which is the
// normal case for every process after the first and a race for two starting at
// once. Treating that as an error means a second replica cannot start.
func Open(nc *nats.Conn, bucket, sessionID string, options ...Option) (*Memory, error) {
	kv, err := natskv.OpenBucket(nc, bucket)
	if err != nil {
		return nil, err
	}

	return New(kv, sessionID, options...)
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

// Save retries a write that lost a race this many times in all before giving up
// with ErrConcurrentUpdate.
const saveAttempts = 5

// saveBackoff is the base of the random pause before a retried write. It
// doubles with each attempt, so writers that collided spread out.
const saveBackoff = 10 * time.Millisecond

// load retrieves and JSON-decodes the current message list from the KV store,
// with the key's revision for a later compare-and-swap write. It returns an
// empty slice and revision 0 when the key does not exist yet (or was cleared).
// The caller must hold m.mu.
func (m *Memory) load() ([]llm.Message, uint64, error) {
	entry, err := m.kv.Get(m.sessionID)
	if err != nil {
		if errors.Is(err, nats.ErrKeyNotFound) {
			return []llm.Message{}, 0, nil
		}

		return nil, 0, err
	}

	var msgs []llm.Message
	if unmarshalErr := json.Unmarshal(entry.Value(), &msgs); unmarshalErr != nil {
		return nil, 0, unmarshalErr
	}

	return msgs, entry.Revision(), nil
}

// store JSON-encodes the message list and writes it back to the KV store, only
// if the key is still at revision: Create when there was no value, Update
// otherwise. A write that lost a race fails with an error matching
// nats.ErrKeyExists; one too large to store, with a *SessionTooLargeError.
// The caller must hold m.mu.
func (m *Memory) store(msgs []llm.Message, revision uint64) error {
	data, err := json.Marshal(msgs)
	if err != nil {
		return err
	}

	if revision == 0 {
		_, err = m.kv.Create(m.sessionID, data)
	} else {
		_, err = m.kv.Update(m.sessionID, data, revision)
	}

	if tooLarge(err) {
		return &SessionTooLargeError{Session: m.sessionID, Size: len(data), Err: err}
	}

	return err
}

// jsErrMessageTooLarge is the JetStream API error code for a message larger
// than the stream's maximum message size — for a KV bucket, its MaxValueSize.
// nats.go names no error for it.
const jsErrMessageTooLarge nats.ErrorCode = 10054

// tooLarge reports whether a write failed because the value is too big to
// store: over the connection's max_payload, refused by nats.go before sending,
// or over the bucket's MaxValueSize, refused by the server.
func tooLarge(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, nats.ErrMaxPayload) {
		return true
	}

	apiErr, ok := errors.AsType[*nats.APIError](err)

	return ok && apiErr.ErrorCode == jsErrMessageTooLarge
}

// Save appends messages to the session history.
//
// Several processes can save to the same session: the write is a
// compare-and-swap on the key's revision, so the read-modify-write cannot
// overwrite another writer's messages. A write that loses the race reloads the
// history and tries again, up to saveAttempts times, and then fails with
// ErrConcurrentUpdate.
func (m *Memory) Save(ctx context.Context, messages []llm.Message) error {
	if len(messages) == 0 {
		return nil
	}

	// The mutex is not what makes Save safe — the revision check is — but it
	// keeps goroutines of this process from colliding with each other.
	m.mu.Lock()
	defer m.mu.Unlock()

	var (
		summary summaryCache
		lastErr error
	)

	for attempt := range saveAttempts {
		if attempt > 0 {
			if err := sleepBackoff(ctx, attempt); err != nil {
				return err
			}
		}

		existing, revision, err := m.load()
		if err != nil {
			return err
		}

		merged, err := m.merge(ctx, existing, messages, &summary)
		if err != nil {
			return err
		}

		err = m.store(merged, revision)
		if err == nil {
			return nil
		}

		if !errors.Is(err, nats.ErrKeyExists) {
			return err
		}

		lastErr = err
	}

	return fmt.Errorf("%w: %w", ErrConcurrentUpdate, lastErr)
}

// merge appends messages to existing and, when the result is over the
// threshold, replaces its oldest messages with a summary.
//
// The summary is an LLM call, so a retried Save does not pay for it again when
// the messages it covers are the same as last time: another writer that won
// the race normally appended at the end, leaving the oldest messages alone.
// It is recomputed only when they differ (another writer summarized first).
func (m *Memory) merge(
	ctx context.Context,
	existing, messages []llm.Message,
	summary *summaryCache,
) ([]llm.Message, error) {
	merged := append(existing, messages...)

	if !m.needSummarization(len(merged)) {
		return merged, nil
	}

	toSummarize := merged[:m.summarySize]
	toAppend := merged[m.summarySize:]

	key, err := json.Marshal(toSummarize)
	if err != nil {
		return nil, err
	}

	if summary.message == nil || !bytes.Equal(summary.key, key) {
		history := memory.FormatSummaryPrompt(toSummarize)

		summaryMsg, llmErr := m.llm.Execute(ctx, []llm.Message{history})
		if llmErr != nil {
			return nil, llmErr
		}

		msg := llm.SystemMessage(memory.SummarySystemMessagePrefix + summaryMsg.Message.TextContent())
		summary.key, summary.message = key, &msg
	}

	result := make([]llm.Message, 0, 1+len(toAppend))
	result = append(result, *summary.message)
	result = append(result, toAppend...)

	return result, nil
}

// summaryCache remembers the last summary Save computed and the messages it
// covers, so a retry can reuse it.
type summaryCache struct {
	key     []byte
	message *llm.Message
}

// sleepBackoff waits a random time up to saveBackoff doubled per attempt, or
// until ctx ends.
func sleepBackoff(ctx context.Context, attempt int) error {
	d := time.Duration(rand.Int64N(int64(saveBackoff << attempt))) //nolint:gosec // jitter, not security

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Retrieve returns all messages currently in memory, ordered from oldest to newest.
//
// query is ignored (matching the behaviour of memory/simple and memory/psql).
func (m *Memory) Retrieve(_ context.Context, _ string) ([]llm.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	msgs, _, err := m.load()

	return msgs, err
}

// Clear removes all messages for this session.
func (m *Memory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.kv.Purge(m.sessionID); err != nil && !errors.Is(err, nats.ErrKeyNotFound) {
		return err
	}

	return nil
}
