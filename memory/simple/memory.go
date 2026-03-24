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

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
)

var _ memory.Memory = (*Memory)(nil)

// Option configures a Memory instance.
type Option func(*Memory)

// Memory stores recent llm.Message values in a bounded FIFO buffer.
type Memory struct {
	buffer *ringBuffer[llm.Message]
	llm    llm.LLM

	capacity         uint
	summaryThreshold uint
	summarySize      uint
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

// WithSummarization enables automatic summarization of memory when the number of stored messages exceeds summarizeThreshold.
//
// When enabled, the Memory will use the provided summaryLLM to generate a summary of the conversation history once the number of stored messages exceeds summarizeThreshold. The summary will be stored as a system message in memory, and the most recent messages up to summarySize will be retained alongside the summary.
//
// If summarizeThreshold is set to 0 or greater than capacity, summarization will trigger when the buffer is full. If summarySize is set to 0, it will default to half of summarizeThreshold (rounded down). To avoid an infinite summarization loop, summarySize must be less than summarizeThreshold.
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
	for _, message := range messages {
		m.buffer.Add(message)
	}

	if m.needSummarization() {
		// get messages to summarize (all but the most recent summarySize)
		toSummarize := m.buffer.Get()
		toAppend := toSummarize[m.summarySize:]
		toSummarize = toSummarize[:m.summarySize]

		history := memory.FormatSummaryPrompt(toSummarize)

		summaryMsg, err := m.llm.Execute(ctx, []llm.Message{history}, nil)
		if err != nil {
			return err
		}

		messagesToStore := []llm.Message{{
			Role:    llm.ChatMessageRoleSystem,
			Content: memory.SummarySystemMessagePrefix + summaryMsg.Message.Content,
		}}

		messagesToStore = append(messagesToStore, toAppend...)

		m.buffer.Replace(messagesToStore)
	}

	return nil
}

// Retrieve returns all messages currently in memory, ordered from oldest to newest.
func (m *Memory) Retrieve(_ context.Context, _ string) ([]llm.Message, error) {
	return m.buffer.Get(), nil
}

// Clear removes all messages from memory.
func (m *Memory) Clear(_ context.Context) error {
	m.buffer.Clear()
	return nil
}

// Len returns the number of messages currently stored.
func (m *Memory) Len() int {
	return m.buffer.Len()
}
