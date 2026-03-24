package jsonfile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
)

var _ memory.Memory = (*Memory)(nil)

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

	var msgs []llm.Message
	if err := json.Unmarshal(data, &msgs); err != nil {
		return err
	}

	m.messages = msgs
	return nil
}

// persist atomically writes the current message slice to the JSON file.
// It writes to a temp file in the same directory first, then renames it over
// the target to avoid partial writes corrupting the stored messages on crash.
// The caller must hold the write lock.
func (m *Memory) persist() error {
	data, err := json.MarshalIndent(m.messages, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.filePath)
	tmp, err := os.CreateTemp(dir, ".memory-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpName, m.filePath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename temp file: %w", err)
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
		toSummarize := m.messages
		toAppend := toSummarize[m.summarySize:]
		toSummarize = toSummarize[:m.summarySize]

		history := memory.FormatSummaryPrompt(toSummarize)

		summaryMsg, err := m.llm.Execute(ctx, []llm.Message{history}, nil)
		if err != nil {
			return err
		}

		m.messages = []llm.Message{
			{
				Role:    llm.ChatMessageRoleSystem,
				Content: memory.SummarySystemMessagePrefix + summaryMsg.Message.Content,
			},
		}
		m.messages = append(m.messages, toAppend...)
	}

	return m.persist()
}

// Retrieve returns all messages currently in memory, ordered from oldest to newest.
func (m *Memory) Retrieve(_ context.Context, _ string) ([]llm.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]llm.Message, len(m.messages))
	copy(result, m.messages)
	return result, nil
}

// Clear removes all messages from memory and truncates the file.
func (m *Memory) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.messages = nil
	return m.persist()
}

// Len returns the number of messages currently stored.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.messages)
}
