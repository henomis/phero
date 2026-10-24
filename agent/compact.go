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

package agent

import (
	"context"
	"slices"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
)

// DefaultKeepRecent is the number of most recent messages a SummaryCompactor
// keeps whole when no WithKeepRecent option is given.
const DefaultKeepRecent = 20

// Compactor shortens the conversation of a running agent so it fits the
// model's context window.
//
// Compact receives the session without the agent's system prompt and returns
// the messages to continue with. The result must stay a valid conversation:
// no tool result without the assistant message that made its call before it.
type Compactor interface {
	Compact(ctx context.Context, messages []llm.Message) ([]llm.Message, error)
}

// CompactorFunc adapts a function to the Compactor interface.
type CompactorFunc func(ctx context.Context, messages []llm.Message) ([]llm.Message, error)

// Compact calls f.
func (f CompactorFunc) Compact(ctx context.Context, messages []llm.Message) ([]llm.Message, error) {
	return f(ctx, messages)
}

// SummaryCompactor is a Compactor that replaces the older part of the
// conversation with a summary written by an LLM, and keeps the most recent
// messages whole.
type SummaryCompactor struct {
	llm        llm.LLM
	keepRecent int
}

// SummaryCompactorOption configures a SummaryCompactor.
type SummaryCompactorOption func(*SummaryCompactor)

// WithKeepRecent sets how many of the most recent messages are kept whole.
// The default is DefaultKeepRecent.
func WithKeepRecent(n int) SummaryCompactorOption {
	return func(c *SummaryCompactor) { c.keepRecent = max(n, 0) }
}

// NewSummaryCompactor returns a SummaryCompactor that writes its summaries
// with summaryLLM, which can be a cheaper model than the agent's.
func NewSummaryCompactor(summaryLLM llm.LLM, opts ...SummaryCompactorOption) *SummaryCompactor {
	c := &SummaryCompactor{llm: summaryLLM, keepRecent: DefaultKeepRecent}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Compact summarizes all but the last keepRecent messages. The split never
// separates a tool call from its results (see memory.SplitPoint), and the
// latest user message among the summarized ones is kept verbatim after the
// summary, so the task the agent works on is not reduced to a summary line.
// The summary prompt lists tool calls and results, so it records the files
// touched, the commands run and what is left to do.
//
// Messages are returned as they are when there is nothing to summarize.
func (c *SummaryCompactor) Compact(ctx context.Context, messages []llm.Message) ([]llm.Message, error) {
	split := memory.SplitPoint(messages, len(messages)-c.keepRecent)
	if split == 0 {
		return messages, nil
	}

	older, recent := messages[:split], messages[split:]

	res, err := c.llm.Execute(ctx, []llm.Message{memory.FormatSummaryPrompt(older)})
	if err != nil {
		return nil, &CompactionError{Err: err}
	}

	if res == nil || res.Message == nil {
		return nil, &CompactionError{Err: ErrIncompleteStream}
	}

	out := make([]llm.Message, 0, len(recent)+2) //nolint:mnd // summary and task
	out = append(out, llm.SystemMessage(memory.SummarySystemMessagePrefix+res.Message.TextContent()))

	if !slices.ContainsFunc(recent, isUserMessage) {
		if task, ok := lastUserMessage(older); ok {
			out = append(out, task)
		}
	}

	return append(out, recent...), nil
}

// isUserMessage reports whether m is a user message.
func isUserMessage(m llm.Message) bool {
	return m.Role == llm.RoleUser
}

// lastUserMessage returns the last user message of messages.
func lastUserMessage(messages []llm.Message) (llm.Message, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if isUserMessage(messages[i]) {
			return messages[i], true
		}
	}

	return llm.Message{}, false
}
