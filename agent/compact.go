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
	"fmt"
	"slices"
	"strings"

	"github.com/henomis/phero/v2/internal/summary"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	"github.com/henomis/phero/v2/trace"
)

// DefaultKeepRecent is the number of most recent messages a SummaryCompactor
// keeps whole when no WithSummaryKeepRecent option is given.
const DefaultKeepRecent = 20

// DefaultKeepTokens bounds the messages a SummaryCompactor keeps, in
// estimated tokens (llm.EstimateTokens), when neither WithSummaryKeepTokens nor the
// CompactInput gives a budget.
const DefaultKeepTokens = 20_000

// keptToolResultChars is how long a tool result kept after a summary can be:
// a longer one is cut to its start and end, so a few large results cannot
// keep the session over the limit by themselves.
const keptToolResultChars = 8_000

// CompactInput is what a Compactor compacts.
type CompactInput struct {
	// Previous is the snapshot the session already starts from, from an
	// earlier compaction or from memory; nil for none. A Compactor updates
	// its summary rather than losing it.
	Previous *memory.Snapshot
	// Messages are the session's messages after the previous snapshot,
	// oldest first, without the system prompt.
	Messages []llm.Message
	// KeepTokens is the budget for the messages kept whole, in estimated
	// tokens: a quarter of the agent's compaction threshold. 0 lets the
	// Compactor choose.
	KeepTokens int
}

// Compaction is what a Compactor returns: a snapshot that stands for the
// older messages, and the newer messages kept after it.
type Compaction struct {
	// Snapshot stands for the input messages that are not kept, together
	// with what the previous snapshot stood for. Its UpTo is set by the
	// memory that stores it.
	Snapshot memory.Snapshot
	// Kept are the last len(Kept) input messages, in order. A Compactor may
	// shorten them, such as a large tool result, but not drop, add or reorder
	// them, and the first may not be a tool result, whose call would be
	// summarized away: the agent checks both (ErrInvalidCompaction).
	Kept []llm.Message
}

// CompactionTrigger says why the agent compacts its session.
type CompactionTrigger int

const (
	// CompactThreshold means the session reached the SetCompaction
	// threshold before an LLM call.
	CompactThreshold CompactionTrigger = iota
	// CompactContextLength means an LLM call failed with
	// llm.ErrContextLengthExceeded; the call is made again after it.
	CompactContextLength
	// CompactManual means Agent.Compact was called.
	CompactManual
)

// String returns "threshold", "context length" or "manual".
func (t CompactionTrigger) String() string {
	switch t {
	case CompactThreshold:
		return "threshold"
	case CompactContextLength:
		return "context length"
	case CompactManual:
		return "manual"
	default:
		return fmt.Sprintf("CompactionTrigger(%d)", int(t))
	}
}

// CompactionInfo describes a compaction of a running agent's session, in an
// EventCompactionStart or EventCompactionEnd. Token counts are estimated with
// llm.EstimateTokens, so the sizes before and after compare. The session counts
// every message, the system prompt and the summary included.
type CompactionInfo struct {
	// Trigger says why the session is compacted.
	Trigger CompactionTrigger
	// TokensBefore and MessagesBefore are the session's size before the
	// compaction.
	TokensBefore   int
	MessagesBefore int
	// TokensAfter and MessagesAfter are its size after it (EventCompactionEnd).
	// They equal the sizes before when the session was not compacted.
	TokensAfter   int
	MessagesAfter int
	// Compacted reports whether the session was replaced
	// (EventCompactionEnd): false when the compactor found nothing to
	// compact, or failed.
	Compacted bool
	// Err is the error the compaction failed with (EventCompactionEnd); the
	// run then ends with it.
	Err error
}

// Compactor shortens the conversation of a running agent so it fits the
// model's context window. It returns a nil Compaction and no error when there
// is nothing to compact, and the session is then left as it is.
type Compactor interface {
	Compact(ctx context.Context, in CompactInput) (*Compaction, error)
}

// CompactorFunc adapts a function to the Compactor interface.
type CompactorFunc func(ctx context.Context, in CompactInput) (*Compaction, error)

// Compact calls f.
func (f CompactorFunc) Compact(ctx context.Context, in CompactInput) (*Compaction, error) {
	return f(ctx, in)
}

// check returns ErrInvalidCompaction unless c's kept messages line up with
// the last messages of in and do not start with a tool result.
func (c *Compaction) check(in CompactInput) error {
	if len(c.Kept) > 0 && len(c.Kept) >= len(in.Messages) {
		return fmt.Errorf("%w: it keeps every message", ErrInvalidCompaction)
	}

	if len(c.Kept) > 0 && c.Kept[0].Role == llm.RoleTool {
		return fmt.Errorf("%w: the kept messages start with a tool result", ErrInvalidCompaction)
	}

	offset := len(in.Messages) - len(c.Kept)

	for i, kept := range c.Kept {
		orig := in.Messages[offset+i]
		if kept.Role != orig.Role || kept.ToolCallID != orig.ToolCallID || !sameToolCalls(kept, orig) {
			return fmt.Errorf("%w: kept message %d is not the input's message %d", ErrInvalidCompaction, i, offset+i)
		}
	}

	return nil
}

// sameToolCalls reports whether a and b make the same tool calls.
func sameToolCalls(a, b llm.Message) bool {
	return slices.EqualFunc(a.ToolCalls, b.ToolCalls, func(x, y llm.ToolCall) bool { return x.ID == y.ID })
}

// SummaryCompactor is a Compactor that replaces the older part of the
// conversation with a summary written by an LLM, and keeps the most recent
// messages whole.
type SummaryCompactor struct {
	llm           llm.LLM
	keepRecent    int
	keepTokens    int
	summaryTokens int
	instructions  string
}

// SummaryCompactorOption configures a SummaryCompactor.
type SummaryCompactorOption func(*SummaryCompactor)

// WithSummaryKeepRecent sets how many of the most recent messages are kept
// whole, at most. The default is DefaultKeepRecent.
func WithSummaryKeepRecent(n int) SummaryCompactorOption {
	return func(c *SummaryCompactor) { c.keepRecent = max(n, 0) }
}

// WithSummaryKeepTokens bounds the kept messages in estimated tokens,
// overriding the agent's budget (CompactInput.KeepTokens). Without either,
// the bound is DefaultKeepTokens.
func WithSummaryKeepTokens(n int) SummaryCompactorOption {
	return func(c *SummaryCompactor) { c.keepTokens = max(n, 0) }
}

// WithSummaryMaxTokens bounds the summary: the prompt asks for at most n
// tokens, and the call's output limit (llm.WithMaxOutputTokens) is half as
// much again, so a summary a little over the budget is not cut. A summary cut
// at that limit anyway is dropped, and the summary is asked for once more
// without the limit: a long summary is better than one that stops halfway.
// The default, 0, sets no bound.
func WithSummaryMaxTokens(n int) SummaryCompactorOption {
	return func(c *SummaryCompactor) { c.summaryTokens = max(n, 0) }
}

// WithSummaryPrompt replaces the instructions the summary is asked with: what
// the summary is for, its sections and its style, for example sections that
// fit a coding agent or a support chat. The compactor adds the previous
// summary and the conversation after them, with each tool call and result.
// The default asks for a domain-neutral summary of an agent's work, in fixed
// sections: task, decisions and facts, work done, user preferences, open
// items. An empty instructions keeps the default.
func WithSummaryPrompt(instructions string) SummaryCompactorOption {
	return func(c *SummaryCompactor) { c.instructions = instructions }
}

// NewSummaryCompactor returns a SummaryCompactor that writes its summaries
// with summaryLLM, which can be a cheaper model than the agent's. Each summary
// call is traced as a model call span under the compaction's memory span, so
// its usage and cost count in the run's Summary.
func NewSummaryCompactor(summaryLLM llm.LLM, opts ...SummaryCompactorOption) *SummaryCompactor {
	c := &SummaryCompactor{llm: trace.InstrumentLLM(summaryLLM), keepRecent: DefaultKeepRecent}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Compact summarizes all but the most recent messages: at most keepRecent of
// them, fewer when they pass the token budget, with long tool results cut to
// keptToolResultChars. The split never separates a tool call from its
// results (see memory.SplitPoint).
//
// The summary updates the previous snapshot's, from a prompt that lists tool
// calls and results (see WithSummaryPrompt), so it records what the agent did
// and what is left to do. The task stays verbatim: when the kept
// messages hold no user message, the latest summarized one, or else the
// previous snapshot's pinned messages, are pinned to the snapshot.
//
// It returns nil when there is nothing to summarize.
func (c *SummaryCompactor) Compact(ctx context.Context, in CompactInput) (*Compaction, error) {
	messages := in.Messages
	split := memory.SplitPoint(messages, c.keepFrom(in))

	if split == 0 {
		return nil, nil //nolint:nilnil // nothing to compact, as the Compactor contract says
	}

	older, recent := messages[:split], messages[split:]

	var previous string
	if in.Previous != nil {
		previous = in.Previous.Summary
	}

	summary, err := c.summarize(ctx, previous, older)
	if err != nil {
		return nil, err
	}

	snapshot := memory.Snapshot{Summary: summary}

	if !slices.ContainsFunc(recent, isUserMessage) {
		if task, ok := lastUserMessage(older); ok {
			snapshot.Pinned = []llm.Message{task}
		} else if in.Previous != nil {
			snapshot.Pinned = in.Previous.Pinned
		}
	}

	return &Compaction{Snapshot: snapshot, Kept: clipToolResults(recent)}, nil
}

// summarize asks the LLM for the summary that updates previous with older,
// within the summary budget when there is one (see WithSummaryMaxTokens).
func (c *SummaryCompactor) summarize(ctx context.Context, previous string, older []llm.Message) (string, error) {
	prompt := c.prompt(previous, older)

	var opts []llm.CallOption

	if c.summaryTokens > 0 {
		words := c.summaryTokens * 3 / 4 //nolint:mnd // about three words in four tokens
		prompt.Parts[0].Text += fmt.Sprintf(summaryBudgetPrompt, c.summaryTokens, words)
		opts = append(opts, llm.WithMaxOutputTokens(c.summaryTokens+c.summaryTokens/2)) //nolint:mnd // headroom
	}

	res, err := c.llm.Execute(ctx, []llm.Message{prompt}, opts...)
	if err == nil && len(opts) > 0 && res != nil && res.FinishReason == llm.FinishLength {
		// Cut at the limit: ask again without it.
		res, err = c.llm.Execute(ctx, []llm.Message{prompt})
	}

	if err != nil {
		return "", &CompactionError{Err: err}
	}

	if res == nil || res.Message == nil {
		return "", &CompactionError{Err: ErrIncompleteStream}
	}

	return res.Message.TextContent(), nil
}

// prompt builds the request that updates previous, the summary of an earlier
// snapshot (empty for none), with conversation.
func (c *SummaryCompactor) prompt(previous string, conversation []llm.Message) llm.Message {
	instructions := c.instructions
	if strings.TrimSpace(instructions) == "" {
		instructions = defaultSummaryInstructions
	}

	if strings.TrimSpace(previous) == "" {
		previous = "(none)"
	}

	return llm.UserMessage(llm.Text(fmt.Sprintf("%s\n\n# PREVIOUS SUMMARY\n%s\n\n# NEW CONVERSATION\n%s",
		strings.TrimSpace(instructions), previous, summary.Conversation(conversation))))
}

// defaultSummaryInstructions are the summary prompt's instructions unless
// WithSummaryPrompt replaces them.
const defaultSummaryInstructions = `# Role
You maintain the working memory of an AI agent in the middle of a long task. Write an updated
summary that lets the agent continue without the messages it replaces.

# Task
Update the previous summary (if any) with the new conversation below. Keep everything from the
previous summary that is still true; replace what changed. Use exactly these sections:

## Task
The user's goal and the current sub-task, in their words where it matters.

## Decisions and facts
Decisions taken and why, constraints, and facts learned.

## Work done
Actions taken and their outcome: the tool calls that mattered and what they used or changed,
errors met and whether they were resolved.

## User preferences
Explicit or implicit preferences about the work and the answers.

## Open items / next steps
What is left to do, pending questions, and the next step the agent was about to take.

# Constraints
- Be dense and factual. Use Markdown bullet points under each section.
- No filler such as "The user and the assistant discussed".
- If something changed, record only its latest state.`

// summaryBudgetPrompt asks for a summary within a budget, in tokens and words.
const summaryBudgetPrompt = `

# LENGTH
Keep the summary under %d tokens (about %d words). Prefer short bullet points; drop detail before dropping sections.`

// keepFrom returns the index of the first message to keep: keepRecent from
// the end, moved later while the kept messages pass the token budget.
func (c *SummaryCompactor) keepFrom(in CompactInput) int {
	budget := c.keepTokens
	if budget <= 0 {
		budget = in.KeepTokens
	}

	if budget <= 0 {
		budget = DefaultKeepTokens
	}

	start := max(len(in.Messages)-c.keepRecent, 0)
	for start < len(in.Messages) && llm.EstimateTokens(clipToolResults(in.Messages[start:])) > budget {
		start++
	}

	return start
}

// clipToolResults returns messages with each tool result's text cut to
// keptToolResultChars, keeping its start and end.
func clipToolResults(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, len(messages))

	for i, m := range messages {
		out[i] = m
		if m.Role != llm.RoleTool {
			continue
		}

		out[i].Parts = make([]llm.ContentPart, len(m.Parts))
		for j, p := range m.Parts {
			if p.Type == llm.ContentTypeText {
				p.Text = clipMiddle(p.Text, keptToolResultChars)
			}

			out[i].Parts[j] = p
		}
	}

	return out
}

// clipMiddle returns s cut to about limit runes, keeping its start and end.
func clipMiddle(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	half := limit / 2 //nolint:mnd // half for the start, half for the end

	return string(runes[:half]) + fmt.Sprintf("\n[... %d characters cut ...]\n", len(runes)-2*half) +
		string(runes[len(runes)-half:])
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
