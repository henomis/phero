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

package simple

import (
	"context"
	"sync"

	"github.com/henomis/phero/v2/internal/snaplog"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
)

var _ memory.SnapshotStore = (*Memory)(nil)

// Option configures a Memory instance.
type Option func(*Memory)

// Memory stores recent llm.Message values in a bounded FIFO buffer. It is a
// memory.SnapshotStore: a snapshot stands for the oldest messages, which are
// kept unless WithPruneCompacted is set.
type Memory struct {
	buffer *ringBuffer[llm.Message]
	llm    llm.LLM

	capacity         uint
	summaryThreshold uint
	summarySize      uint

	mu sync.Mutex // guards next and snapshot, and orders Save with them
	// next is the absolute position of the next message saved: the buffer
	// holds positions next-Len() to next-1, so a cursor survives eviction.
	next     int
	snapshot *memory.Snapshot
	prune    bool
}

// WithPruneCompacted drops the messages a snapshot stands for when it is
// saved. Off by default: the buffer keeps them until they are evicted.
func WithPruneCompacted(prune bool) Option {
	return func(m *Memory) {
		m.prune = prune
	}
}

// New creates a new Memory that keeps up to maxItems messages.
func New(maxItems uint, options ...Option) *Memory {
	m := &Memory{
		buffer:   newRingBuffer[llm.Message](int(maxItems)),
		capacity: maxItems,
	}

	for _, option := range options {
		option(m)
	}

	return m
}

// WithSummarization enables automatic summarization of memory when the number of stored
// messages exceeds summarizeThreshold.
//
// When enabled, the Memory will use the provided summaryLLM to generate a summary of the
// conversation history once the number of stored messages exceeds summarizeThreshold. The
// oldest summarySize messages are replaced by the summary, stored as a system message, and
// the newer messages are kept after it. The split moves forward past tool results, so a
// tool call and its results are summarized or kept together.
//
// If summarizeThreshold is set to 0 or greater than capacity, summarization will trigger when
// the buffer is full. If summarySize is set to 0, it will default to half of
// summarizeThreshold (rounded down). To avoid an infinite summarization loop, summarySize
// must be less than summarizeThreshold.
//
// Deprecated: use agent.SetCompaction with this memory instead: it keeps
// snapshots (memory.SnapshotStore), so the agent's summary is stored and the
// log is not rewritten. Summarization here counts messages rather than
// tokens, calls the LLM inside Save, and replaces the oldest messages. It
// keeps working as before, and cannot be combined with snapshots
// (memory.ErrSnapshotsUnsupported).
func WithSummarization(summaryLLM llm.LLM, summarizeThreshold, summarySize uint) Option {
	return func(m *Memory) {
		m.llm = summaryLLM

		if summarizeThreshold == 0 || summarizeThreshold > m.capacity {
			summarizeThreshold = m.capacity
		}

		// if summarySize is zero, derive a default value from summarizeThreshold
		// To avoid an infinite summarization loop, summarySize must be less than summarizeThreshold.
		m.summarySize = memory.ClampSummarySize(summarizeThreshold, summarySize)
		m.summaryThreshold = summarizeThreshold
	}
}

func (m *Memory) needSummarization() bool {
	return m.llm != nil && m.summaryThreshold > 0 && m.buffer.Len() >= int(m.summaryThreshold)
}

// Save adds messages to memory, evicting oldest if capacity is exceeded.
func (m *Memory) Save(ctx context.Context, messages []llm.Message) error {
	m.mu.Lock()

	for _, message := range messages {
		m.buffer.Add(message)
	}

	m.next += len(messages)
	m.mu.Unlock()

	if m.needSummarization() {
		// Summarize the oldest summarySize messages, moved past tool results
		// so none is kept without its tool call.
		buffered := m.buffer.Get()
		toSummarize := buffered[memory.SplitPoint(buffered, 0):]
		split := memory.SplitPoint(toSummarize, int(m.summarySize))
		toAppend := toSummarize[split:]
		toSummarize = toSummarize[:split]

		history := memory.FormatSummaryPrompt(toSummarize)

		summaryMsg, err := m.llm.Execute(ctx, []llm.Message{history})
		if err != nil {
			return err
		}

		messagesToStore := make([]llm.Message, 0, 1+len(toAppend))
		messagesToStore = append(messagesToStore, llm.SystemMessage(
			memory.SummarySystemMessagePrefix+summaryMsg.Message.TextContent(),
		))
		messagesToStore = append(messagesToStore, toAppend...)

		m.buffer.Replace(messagesToStore)
	}

	return nil
}

// Retrieve returns all messages currently in memory, ordered from oldest to newest.
// Tool results at the start, whose tool call was evicted from the buffer, are
// left out: providers reject a tool result without its call. With a snapshot,
// the messages it stands for are replaced by memory.SummaryMessages.
func (m *Memory) Retrieve(_ context.Context, _ string) ([]llm.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.snapshot == nil || m.llm != nil {
		buffered := m.buffer.Get()

		return buffered[memory.SplitPoint(buffered, 0):], nil
	}

	msgs := m.buffer.Get()

	return snaplog.View(msgs, m.next-len(msgs), *m.snapshot), nil
}

// LoadSnapshot returns the latest snapshot (nil when there is none) and the
// messages after it, with their cursors. It returns
// memory.ErrSnapshotsUnsupported with WithSummarization, which rewrites the
// buffer.
func (m *Memory) LoadSnapshot(_ context.Context) (*memory.Snapshot, []memory.LoggedMessage, error) {
	if m.llm != nil {
		return nil, nil, memory.ErrSnapshotsUnsupported
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.snapshot == nil {
		return nil, m.tail(), nil
	}

	snap := *m.snapshot

	return &snap, m.tail(), nil
}

// SaveSnapshot stores s as standing for every message up to and including
// upTo, when the latest snapshot is still prev (memory.ErrSnapshotConflict
// otherwise). upTo must be a cursor of this memory after prev's, which may
// have been evicted since (memory.ErrInvalidCursor otherwise).
func (m *Memory) SaveSnapshot(_ context.Context, s memory.Snapshot, upTo memory.Cursor, prev *memory.Snapshot) error {
	if m.llm != nil {
		return memory.ErrSnapshotsUnsupported
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := snaplog.Cutoff(m.snapshot, prev, upTo, m.next); err != nil {
		return err
	}

	s.UpTo = upTo
	m.snapshot = &s

	if m.prune {
		kept := make([]llm.Message, 0, m.buffer.Len())
		for _, lm := range m.tail() {
			kept = append(kept, lm.Message)
		}

		m.buffer.Replace(kept)
	}

	return nil
}

// tail returns the buffered messages after the snapshot, with their
// cursors. The caller holds m.mu.
func (m *Memory) tail() []memory.LoggedMessage {
	msgs := m.buffer.Get()

	return snaplog.Tail(msgs, m.next-len(msgs), m.snapshot)
}

// Clear removes all messages and the snapshot from memory.
func (m *Memory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.buffer.Clear()
	m.snapshot = nil

	return nil
}

// Len returns the number of messages currently stored.
func (m *Memory) Len() int {
	return m.buffer.Len()
}
