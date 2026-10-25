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

package jsonfile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/henomis/phero/v2/internal/snaplog"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
)

var _ memory.SnapshotStore = (*Memory)(nil)

// fileVersion is the file format with a snapshot: an object holding the
// messages, the absolute position of the first one, and the snapshot. A file
// without a snapshot stays a plain JSON array of messages (version 1), which
// older versions read.
const fileVersion = 2

// fileV2 is the version 2 file.
type fileV2 struct {
	Version  int              `json:"version"`
	Offset   int              `json:"offset"`
	Messages []llm.Message    `json:"messages"`
	Snapshot *memory.Snapshot `json:"snapshot,omitempty"`
}

// Option configures a Memory instance.
type Option func(*Memory)

// Memory stores llm.Message values in an unbounded slice and persists them
// as JSON to the file at filePath.
type Memory struct {
	mu       sync.RWMutex
	messages []llm.Message
	filePath string
	llm      llm.LLM

	summaryThreshold uint
	summarySize      uint

	// offset is the absolute position of messages[0]: pruned messages keep
	// their positions, so cursors stay valid.
	offset   int
	snapshot *memory.Snapshot
	prune    bool
}

// WithPruneCompacted drops the messages a snapshot stands for when it is
// saved, and rewrites the file without them. Off by default: the file keeps
// the whole log.
func WithPruneCompacted(prune bool) Option {
	return func(m *Memory) {
		m.prune = prune
	}
}

// New creates a Memory that persists all messages as JSON to filePath.
// If the file already exists its contents are loaded automatically.
func New(filePath string, options ...Option) (*Memory, error) {
	if filePath == "" {
		return nil, ErrEmptyFilePath
	}

	m := &Memory{
		filePath: filePath,
	}

	for _, option := range options {
		option(m)
	}

	if err := m.load(); err != nil {
		return nil, err
	}

	return m, nil
}

// WithSummarization enables automatic summarization of memory when the number of stored
// messages exceeds summarizeThreshold.
//
// This mirrors the behavior of memory/simple.WithSummarization: the oldest summarySize
// messages, moved past tool results, are replaced by a summary.
func WithSummarization(summaryLLM llm.LLM, summarizeThreshold, summarySize uint) Option {
	return func(m *Memory) {
		m.llm = summaryLLM

		m.summarySize = memory.ClampSummarySize(summarizeThreshold, summarySize)
		m.summaryThreshold = summarizeThreshold
	}
}

// load reads messages from the JSON file, if it exists.
func (m *Memory) load() error {
	data, err := os.ReadFile(m.filePath)
	if os.IsNotExist(err) {
		return nil
	}

	if err != nil {
		return err
	}

	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		var f fileV2
		if unmarshalErr := json.Unmarshal(data, &f); unmarshalErr != nil {
			return unmarshalErr
		}

		if f.Version != fileVersion {
			return fmt.Errorf("%w: %d", ErrUnknownFileVersion, f.Version)
		}

		m.messages, m.offset, m.snapshot = f.Messages, f.Offset, f.Snapshot

		return nil
	}

	var msgs []llm.Message
	if unmarshalErr := json.Unmarshal(data, &msgs); unmarshalErr != nil {
		return unmarshalErr
	}

	m.messages = msgs

	return nil
}

// encode returns the file contents: a plain array without a snapshot, the
// version 2 object with one.
func (m *Memory) encode() ([]byte, error) {
	if m.snapshot == nil && m.offset == 0 {
		return json.MarshalIndent(m.messages, "", "  ")
	}

	return json.MarshalIndent(fileV2{
		Version: fileVersion, Offset: m.offset, Messages: m.messages, Snapshot: m.snapshot,
	}, "", "  ")
}

// persist atomically writes the current message slice to the JSON file.
// It writes to a temp file in the same directory first, then renames it over
// the target to avoid partial writes corrupting the stored messages on crash.
// The caller must hold the write lock.
func (m *Memory) persist() error {
	data, err := m.encode()
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.filePath)

	tmp, err := os.CreateTemp(dir, ".memory-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tmpName := tmp.Name()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)

		return fmt.Errorf("write temp file: %w", writeErr)
	}

	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)

		return fmt.Errorf("sync temp file: %w", syncErr)
	}

	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", closeErr)
	}

	if renameErr := os.Rename(tmpName, m.filePath); renameErr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", renameErr)
	}

	return nil
}

func (m *Memory) needSummarization() bool {
	return m.llm != nil && m.summaryThreshold > 0 && uint(len(m.messages)) >= m.summaryThreshold
}

// Save appends messages to memory and flushes to disk.
func (m *Memory) Save(ctx context.Context, messages []llm.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = append(m.messages, messages...)

	if m.needSummarization() {
		// Split past tool results so none is kept without its tool call.
		split := memory.SplitPoint(m.messages, int(m.summarySize))
		toSummarize := m.messages[:split]
		toAppend := m.messages[split:]

		history := memory.FormatSummaryPrompt(toSummarize)

		summaryMsg, err := m.llm.Execute(ctx, []llm.Message{history})
		if err != nil {
			return err
		}

		m.messages = []llm.Message{
			llm.SystemMessage(memory.SummarySystemMessagePrefix + summaryMsg.Message.TextContent()),
		}
		m.messages = append(m.messages, toAppend...)
	}

	return m.persist()
}

// Retrieve returns all messages currently in memory, ordered from oldest to
// newest. With a snapshot, the messages it stands for are replaced by
// memory.SummaryMessages.
func (m *Memory) Retrieve(_ context.Context, _ string) ([]llm.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.snapshot != nil && m.llm == nil {
		return snaplog.View(m.messages, m.offset, *m.snapshot), nil
	}

	result := make([]llm.Message, len(m.messages))
	copy(result, m.messages)

	return result, nil
}

// LoadSnapshot returns the latest snapshot (nil when there is none) and the
// messages after it, with their cursors. It returns
// memory.ErrSnapshotsUnsupported with WithSummarization, which rewrites the
// log.
func (m *Memory) LoadSnapshot(_ context.Context) (*memory.Snapshot, []memory.LoggedMessage, error) {
	if m.llm != nil {
		return nil, nil, memory.ErrSnapshotsUnsupported
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	tail := snaplog.Tail(m.messages, m.offset, m.snapshot)
	if m.snapshot == nil {
		return nil, tail, nil
	}

	snap := *m.snapshot

	return &snap, tail, nil
}

// SaveSnapshot stores s as standing for every message up to and including
// upTo and writes the file, when the latest snapshot is still prev
// (memory.ErrSnapshotConflict otherwise). upTo must be a cursor of this
// memory after prev's (memory.ErrInvalidCursor otherwise).
func (m *Memory) SaveSnapshot(_ context.Context, s memory.Snapshot, upTo memory.Cursor, prev *memory.Snapshot) error {
	if m.llm != nil {
		return memory.ErrSnapshotsUnsupported
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	pos, err := snaplog.Cutoff(m.snapshot, prev, upTo, m.offset+len(m.messages))
	if err != nil {
		return err
	}

	oldMessages, oldOffset, oldSnapshot := m.messages, m.offset, m.snapshot

	s.UpTo = upTo
	m.snapshot = &s

	if m.prune && pos >= m.offset {
		m.messages = append([]llm.Message(nil), m.messages[pos+1-m.offset:]...)
		m.offset = pos + 1
	}

	if err = m.persist(); err != nil {
		m.messages, m.offset, m.snapshot = oldMessages, oldOffset, oldSnapshot
		return err
	}

	return nil
}

// Clear removes all messages from memory and truncates the file.
func (m *Memory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = nil
	m.offset = 0
	m.snapshot = nil

	return m.persist()
}

// Len returns the number of messages currently stored.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.messages)
}
