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

package agent_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	"github.com/henomis/phero/v2/memory/simple"
)

// summaryLLM answers every call with "summary" and records the prompts.
type summaryLLM struct {
	prompts []string
	err     error
}

func (s *summaryLLM) Execute(_ context.Context, msgs []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
	if s.err != nil {
		return nil, s.err
	}

	s.prompts = append(s.prompts, llm.TextContent(msgs[len(msgs)-1].Parts...))

	return textResult("summary"), nil
}

// paddedArgs are tool call arguments of about 300 estimated tokens: over the
// budget a compaction keeps whole at a 1000-token threshold, so the messages
// after the summary are worth compacting.
var paddedArgs = `{"pad":"` + strings.Repeat("x", 1200) + `"}`

// toolSession returns a user task followed by n tool turns (assistant call
// plus result).
func toolSession(n int) []llm.Message {
	msgs := make([]llm.Message, 0, 1+2*n)
	msgs = append(msgs, llm.UserMessage(llm.Text("fix the failing test")))

	for i := range n {
		id := string(rune('a' + i))
		msgs = append(msgs,
			llm.AssistantMessage(nil, llm.ToolCall{ID: id, Function: llm.FunctionCall{Name: "bash", Arguments: `{"command":"go test"}`}}),
			llm.ToolResultMessage(id, llm.Text("FAIL "+id)),
		)
	}

	return msgs
}

// assertValidConversation fails when a tool result's call is not before it.
func assertValidConversation(t *testing.T, msgs []llm.Message) {
	t.Helper()

	calls := map[string]bool{}

	for i, m := range msgs {
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = true
		}

		if m.Role == llm.RoleTool && !calls[m.ToolCallID] {
			t.Fatalf("message %d answers tool call %q, which is not before it", i, m.ToolCallID)
		}
	}
}

func TestSummaryCompactor_KeepsRecentAndTask(t *testing.T) {
	sum := &summaryLLM{}
	c := agent.NewSummaryCompactor(sum, agent.WithKeepRecent(3))

	in := agent.CompactInput{Messages: toolSession(5)}

	out, err := c.Compact(context.Background(), in)
	if err != nil || out == nil {
		t.Fatalf("Compact = %+v, %v", out, err)
	}

	// The last 3 messages moved past the tool result at the split:
	// [result d, assistant e, result e] -> [assistant e, result e].
	if len(out.Kept) != 2 || out.Kept[0].Role != llm.RoleAssistant {
		t.Fatalf("kept = %+v, want the last tool turn", out.Kept)
	}

	assertValidConversation(t, out.Kept)

	if out.Snapshot.Summary != "summary" {
		t.Fatalf("summary = %q", out.Snapshot.Summary)
	}

	// The kept messages hold no user message: the task is pinned verbatim.
	if len(out.Snapshot.Pinned) != 1 || out.Snapshot.Pinned[0].TextContent() != "fix the failing test" {
		t.Fatalf("pinned = %+v, want the task", out.Snapshot.Pinned)
	}

	if len(sum.prompts) != 1 || !strings.Contains(sum.prompts[0], "tool call a: bash") {
		t.Fatalf("summary prompt does not list the tool calls: %v", sum.prompts)
	}
}

// limitLLM records each call's output limit and prompt, and answers "cut"
// with FinishLength while cut is set and the call has a limit.
type limitLLM struct {
	limits  []int
	prompts []string
	cut     bool
}

func (l *limitLLM) Execute(_ context.Context, msgs []llm.Message, opts ...llm.CallOption) (*llm.Result, error) {
	limit := llm.NewCallConfig(opts...).MaxOutputTokens
	l.limits = append(l.limits, limit)
	l.prompts = append(l.prompts, msgs[len(msgs)-1].TextContent())

	if l.cut && limit > 0 {
		res := textResult("cut")
		res.FinishReason = llm.FinishLength

		return res, nil
	}

	return textResult("summary"), nil
}

func TestSummaryCompactor_MaxSummaryTokens(t *testing.T) {
	l := &limitLLM{}
	c := agent.NewSummaryCompactor(l, agent.WithKeepRecent(2), agent.WithMaxSummaryTokens(400))

	out, err := c.Compact(context.Background(), agent.CompactInput{Messages: toolSession(4)})
	if err != nil || out == nil || out.Snapshot.Summary != "summary" {
		t.Fatalf("Compact = %+v, %v", out, err)
	}

	if !slices.Equal(l.limits, []int{600}) || !strings.Contains(l.prompts[0], "under 400 tokens") {
		t.Fatalf("limits = %v, prompt = %q; want one call limited to 600 that asks for 400", l.limits, l.prompts[0])
	}

	// Without the option, the call has no limit and the prompt no budget.
	l = &limitLLM{}
	if _, err = agent.NewSummaryCompactor(l, agent.WithKeepRecent(2)).Compact(
		context.Background(), agent.CompactInput{Messages: toolSession(4)}); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(l.limits, []int{0}) || strings.Contains(l.prompts[0], "# LENGTH") {
		t.Fatalf("limits = %v, prompt = %q; want no limit and no budget", l.limits, l.prompts[0])
	}
}

func TestSummaryCompactor_CutSummaryIsAskedAgain(t *testing.T) {
	l := &limitLLM{cut: true}
	c := agent.NewSummaryCompactor(l, agent.WithKeepRecent(2), agent.WithMaxSummaryTokens(400))

	out, err := c.Compact(context.Background(), agent.CompactInput{Messages: toolSession(4)})
	if err != nil || out == nil {
		t.Fatalf("Compact = %+v, %v", out, err)
	}

	if out.Snapshot.Summary != "summary" || !slices.Equal(l.limits, []int{600, 0}) {
		t.Fatalf("summary = %q, limits = %v; want the cut one dropped and a second call without a limit",
			out.Snapshot.Summary, l.limits)
	}
}

func TestSummaryCompactor_UpdatesThePreviousSnapshot(t *testing.T) {
	sum := &summaryLLM{}
	c := agent.NewSummaryCompactor(sum, agent.WithKeepRecent(2))

	task := llm.UserMessage(llm.Text("the original task"))
	previous := &memory.Snapshot{Summary: "- earlier work", Pinned: []llm.Message{task}}

	// Only tool turns since the previous snapshot: no user message to pin.
	out, err := c.Compact(context.Background(), agent.CompactInput{Previous: previous, Messages: toolSession(4)[1:]})
	if err != nil || out == nil {
		t.Fatalf("Compact = %+v, %v", out, err)
	}

	if !strings.Contains(sum.prompts[0], "- earlier work") {
		t.Fatalf("prompt = %q, want the previous summary in it", sum.prompts[0])
	}

	if len(out.Snapshot.Pinned) != 1 || out.Snapshot.Pinned[0].TextContent() != "the original task" {
		t.Fatalf("pinned = %+v, want the previous snapshot's task kept", out.Snapshot.Pinned)
	}
}

func TestSummaryCompactor_KeepsByTokens(t *testing.T) {
	sum := &summaryLLM{}
	c := agent.NewSummaryCompactor(sum, agent.WithKeepRecent(20))

	// Tool results of 40k characters: 20 messages would keep about 100k tokens.
	msgs := toolSession(10)
	for i := range msgs {
		if msgs[i].Role == llm.RoleTool {
			msgs[i] = llm.ToolResultMessage(msgs[i].ToolCallID, llm.Text(strings.Repeat("x", 40_000)))
		}
	}

	out, err := c.Compact(context.Background(), agent.CompactInput{Messages: msgs, KeepTokens: 5_000})
	if err != nil || out == nil {
		t.Fatalf("Compact = %+v, %v", out, err)
	}

	if got := agent.EstimateTokens(out.Kept); got > 5_000 || len(out.Kept) == 0 {
		t.Fatalf("kept %d messages of about %d tokens, want some within the 5000 budget", len(out.Kept), got)
	}

	for _, m := range out.Kept {
		if m.Role == llm.RoleTool && len(m.TextContent()) > agent.DefaultKeptToolResultChars+100 {
			t.Fatalf("kept tool result of %d characters, want it cut", len(m.TextContent()))
		}
	}

	assertValidConversation(t, out.Kept)
}

func TestSummaryCompactor_NothingToSummarize(t *testing.T) {
	sum := &summaryLLM{}

	out, err := agent.NewSummaryCompactor(sum, agent.WithKeepRecent(10)).Compact(context.Background(), agent.CompactInput{Messages: toolSession(1)})
	if err != nil || out != nil || len(sum.prompts) != 0 {
		t.Fatalf("out = %+v, err = %v, prompts = %d; want nothing to compact", out, err, len(sum.prompts))
	}
}

func TestSummaryCompactor_ErrorIsCompactionError(t *testing.T) {
	cause := errors.New("summary model down")

	_, err := agent.NewSummaryCompactor(&summaryLLM{err: cause}, agent.WithKeepRecent(1)).Compact(context.Background(), agent.CompactInput{Messages: toolSession(3)})

	var cErr *agent.CompactionError
	if !errors.As(err, &cErr) || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want a CompactionError wrapping the cause", err)
	}
}

func TestRun_RejectsAnInvalidCompaction(t *testing.T) {
	for name, kept := range map[string]func(in agent.CompactInput) []llm.Message{
		"starts with a tool result": func(in agent.CompactInput) []llm.Message { return in.Messages[len(in.Messages)-1:] },
		"not the last messages":     func(in agent.CompactInput) []llm.Message { return in.Messages[:1] },
		"keeps everything":          func(in agent.CompactInput) []llm.Message { return in.Messages },
	} {
		client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
			return toolCallResultWithUsage("echo", fmt.Sprintf("c%d", call), paddedArgs, &llm.Usage{InputTokens: 5000}), nil
		}}

		a := mustNew(t, client, "agent", "desc")
		addEchoTool(t, a)
		a.SetCompaction(agent.CompactorFunc(func(_ context.Context, in agent.CompactInput) (*agent.Compaction, error) {
			return &agent.Compaction{Snapshot: memory.Snapshot{Summary: "s"}, Kept: kept(in)}, nil
		}), 1000)

		if _, err := a.Run(context.Background(), llm.Text("go")); !errors.Is(err, agent.ErrInvalidCompaction) {
			t.Errorf("%s: err = %v, want ErrInvalidCompaction", name, err)
		}
	}
}

// isSummary reports whether m is the summary message of a snapshot whose
// summary is text.
func isSummary(m llm.Message, text string) bool {
	return m.Role == llm.RoleUser && m.TextContent() == memory.SummarySystemMessagePrefix+text
}

// scriptedLLM answers from fn and records the prompt of every call.
type scriptedLLM struct {
	prompts [][]llm.Message
	fn      func(call int, msgs []llm.Message) (*llm.Result, error)
}

func (s *scriptedLLM) Execute(_ context.Context, msgs []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
	s.prompts = append(s.prompts, msgs)

	return s.fn(len(s.prompts), msgs)
}

// keepLastCompactor summarizes everything but the last message (moved past
// tool results) as "compacted", and counts its calls.
func keepLastCompactor(calls *int) agent.Compactor {
	return agent.CompactorFunc(func(_ context.Context, in agent.CompactInput) (*agent.Compaction, error) {
		*calls++

		split := memory.SplitPoint(in.Messages, len(in.Messages)-1)
		if split == 0 {
			return nil, nil
		}

		return &agent.Compaction{Snapshot: memory.Snapshot{Summary: "compacted"}, Kept: in.Messages[split:]}, nil
	})
}

func addEchoTool(t *testing.T, a *agent.Agent) {
	t.Helper()

	if err := a.AddTool(mustTool(t, "echo", func(context.Context, *struct{}) (string, error) { return "ok", nil })); err != nil {
		t.Fatal(err)
	}
}

func TestRun_CompactsAtTokenThreshold(t *testing.T) {
	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call < 4 {
			return toolCallResultWithUsage("echo", fmt.Sprintf("c%d", call), paddedArgs, &llm.Usage{InputTokens: 900, OutputTokens: 200}), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 1000)

	res, err := a.Run(context.Background(), llm.Text("go"))
	if err != nil || res.TextContent() != "done" {
		t.Fatalf("Run = %v, %v", res, err)
	}

	// Calls 1-3 report 1100 tokens, so calls 2-4 are each preceded by a compaction.
	if compactions != 3 {
		t.Fatalf("compactions = %d, want 3", compactions)
	}

	for i, prompt := range client.prompts[1:] {
		assertValidConversation(t, prompt)

		if !isSummary(prompt[1], "compacted") {
			t.Fatalf("call %d prompt = %+v, want the compacted session", i+2, prompt)
		}
	}
}

func TestRun_CompactsAndRetriesOnContextLengthError(t *testing.T) {
	tooLong := &llm.ProviderError{Provider: "p", StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("prompt is too long")}

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		switch call {
		case 1:
			return toolCallResult("echo", "c1", "{}"), nil
		case 2:
			return nil, tooLong
		default:
			return textResult("done"), nil
		}
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)

	mem := &appendMemory{}
	a.SetMemory(mem)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 0)

	res, err := a.Run(context.Background(), llm.Text("go"))
	if err != nil || res.TextContent() != "done" {
		t.Fatalf("Run = %v, %v", res, err)
	}

	if compactions != 1 || len(client.prompts) != 3 {
		t.Fatalf("compactions = %d, calls = %d; want 1 and 3", compactions, len(client.prompts))
	}

	assertValidConversation(t, client.prompts[2])

	// The summary stays in the run: memory gets the turn as it happened.
	assertRoles(t, mem.msgs, llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleAssistant)
}

// storedHistory is a past turn with a tool call, as memory returns it.
func storedHistory() []llm.Message {
	return []llm.Message{
		llm.UserMessage(llm.Text("u1")),
		llm.AssistantMessage(nil, llm.ToolCall{ID: "c1", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: "echo", Arguments: "{}"}}),
		llm.ToolResultMessage("c1", llm.Text("ok")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("a1")}),
	}
}

// assertRoles fails unless msgs has exactly the given roles, in order.
func assertRoles(t *testing.T, msgs []llm.Message, roles ...string) {
	t.Helper()

	got := make([]string, 0, len(msgs))
	for _, m := range msgs {
		got = append(got, m.Role)
	}

	if !slices.Equal(got, roles) {
		t.Fatalf("roles = %v, want %v", got, roles)
	}
}

func TestRun_CompactionSavesEachMessageOnce(t *testing.T) {
	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call == 1 {
			return toolCallResultWithUsage("echo", "c2", paddedArgs, &llm.Usage{InputTokens: 5000}), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)

	mem := &appendMemory{msgs: storedHistory()}
	a.SetMemory(mem)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 1000)

	if _, err := a.Run(context.Background(), llm.Text("u2")); err != nil {
		t.Fatal(err)
	}

	if compactions != 1 {
		t.Fatalf("compactions = %d, want 1", compactions)
	}

	// The stored history, then this run's turn: no summary, no message twice.
	assertRoles(t, mem.msgs,
		llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleAssistant,
		llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleAssistant)
	assertValidConversation(t, mem.msgs)
}

func TestRun_CompactionWithCheckpointKeepsUnsavedMessages(t *testing.T) {
	tooLong := &llm.ProviderError{Provider: "p", StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("prompt is too long")}

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call == 1 {
			return nil, tooLong
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")

	mem := &appendMemory{msgs: storedHistory()}
	a.SetMemory(mem)
	a.SetCheckpoint(true)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 0)

	if _, err := a.Run(context.Background(), llm.Text("u2")); err != nil {
		t.Fatal(err)
	}

	// The user message was not saved before the compaction: it must be now.
	assertRoles(t, mem.msgs,
		llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleAssistant,
		llm.RoleUser, llm.RoleAssistant)

	if got := mem.msgs[4].TextContent(); got != "u2" {
		t.Fatalf("stored user message = %q, want u2", got)
	}
}

func TestRun_SecondContextLengthErrorEndsRun(t *testing.T) {
	tooLong := &llm.ProviderError{Provider: "p", StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("prompt is too long")}
	client := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) { return nil, tooLong }}

	a := mustNew(t, client, "agent", "desc")

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 0)

	_, err := a.Run(context.Background(), llm.Text("go"))
	if !errors.Is(err, llm.ErrContextLengthExceeded) {
		t.Fatalf("err = %v, want ErrContextLengthExceeded", err)
	}

	if compactions != 1 || len(client.prompts) != 2 {
		t.Fatalf("compactions = %d, calls = %d; want 1 and 2", compactions, len(client.prompts))
	}
}

func TestRun_CompactionErrorEndsRun(t *testing.T) {
	client := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) {
		return toolCallResultWithUsage("echo", "c", paddedArgs, &llm.Usage{InputTokens: 5000}), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)

	cause := errors.New("summary model down")
	a.SetCompaction(agent.NewSummaryCompactor(&summaryLLM{err: cause}, agent.WithKeepRecent(1)), 1000)

	_, err := a.Run(context.Background(), llm.Text("go"))

	var cErr *agent.CompactionError
	if !errors.As(err, &cErr) || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want a CompactionError", err)
	}
}

// compactionEvents runs a with RunStream and returns its compaction events,
// and the run's error.
func compactionEvents(a *agent.Agent) ([]agent.Event, error) {
	var events []agent.Event

	for ev, err := range a.RunStream(context.Background(), llm.Text("go")) {
		if err != nil {
			return events, err
		}

		if ev.Type == agent.EventCompactionStart || ev.Type == agent.EventCompactionEnd {
			events = append(events, ev)
		}
	}

	return events, nil
}

func TestRunStream_CompactionEvents(t *testing.T) {
	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call < 3 {
			return toolCallResultWithUsage("echo", fmt.Sprintf("c%d", call), paddedArgs, &llm.Usage{InputTokens: 900, OutputTokens: 200}), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 1000)

	events, err := compactionEvents(a)
	if err != nil {
		t.Fatal(err)
	}

	// Calls 1-2 report 1100 tokens, so iterations 2 and 3 start with a compaction.
	if len(events) != 4 {
		t.Fatalf("got %d compaction events, want 4", len(events))
	}

	for i := 0; i < len(events); i += 2 {
		start, end := events[i], events[i+1]
		if start.Type != agent.EventCompactionStart || end.Type != agent.EventCompactionEnd {
			t.Fatalf("events %d-%d = %v, %v; want a start then an end", i, i+1, start.Type, end.Type)
		}

		if want := i/2 + 2; start.Iteration != want || end.Iteration != want {
			t.Fatalf("iterations = %d, %d; want %d", start.Iteration, end.Iteration, want)
		}

		info := end.Compaction
		if info.Trigger != agent.CompactThreshold || !info.Compacted || info.Err != nil {
			t.Fatalf("end = %+v, want a successful threshold compaction", info)
		}

		if info.TokensBefore != start.Compaction.TokensBefore || info.TokensAfter >= info.TokensBefore ||
			info.MessagesAfter >= info.MessagesBefore {
			t.Fatalf("end = %+v, want the session smaller than at the start (%+v)", info, start.Compaction)
		}
	}
}

func TestRunStream_CompactionEventOnContextLengthError(t *testing.T) {
	tooLong := &llm.ProviderError{Provider: "p", StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("prompt is too long")}

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		switch call {
		case 1:
			return toolCallResult("echo", "c1", "{}"), nil
		case 2:
			return nil, tooLong
		default:
			return textResult("done"), nil
		}
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 0)

	events, err := compactionEvents(a)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Fatalf("got %d compaction events, want 2", len(events))
	}

	// Iteration 2 failed; the call is made again as iteration 3.
	if end := events[1]; end.Compaction.Trigger != agent.CompactContextLength || end.Iteration != 3 {
		t.Fatalf("end = %+v at iteration %d, want a context-length compaction before iteration 3", end.Compaction, end.Iteration)
	}
}

func TestRunStream_CompactionEventCarriesTheError(t *testing.T) {
	client := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) {
		return toolCallResultWithUsage("echo", "c", paddedArgs, &llm.Usage{InputTokens: 5000}), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)

	cause := errors.New("summary model down")
	a.SetCompaction(agent.NewSummaryCompactor(&summaryLLM{err: cause}, agent.WithKeepRecent(1)), 1000)

	events, err := compactionEvents(a)
	if !errors.Is(err, cause) {
		t.Fatalf("err = %v, want the compaction's", err)
	}

	if len(events) != 2 {
		t.Fatalf("got %d compaction events, want 2", len(events))
	}

	info := events[1].Compaction
	if info.Compacted || !errors.Is(info.Err, cause) || info.MessagesAfter != info.MessagesBefore {
		t.Fatalf("end = %+v, want the error and the session unchanged", info)
	}
}

func TestCompact_StoresTheSnapshotForTheNextRun(t *testing.T) {
	store := simple.New(100)
	if err := store.Save(context.Background(), storedHistory()); err != nil {
		t.Fatal(err)
	}

	client := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) { return textResult("done"), nil }}
	a := mustNew(t, client, "agent", "desc")
	a.SetMemory(store)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 0)

	info, err := a.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if !info.Compacted || info.Trigger != agent.CompactManual || info.Err != nil ||
		info.MessagesBefore != 5 || info.MessagesAfter != 3 || info.TokensBefore == 0 || info.TokensAfter == 0 {
		t.Fatalf("info = %+v, want [system, 4 messages] compacted to [system, summary, last]", info)
	}

	if len(client.prompts) != 0 {
		t.Fatalf("Compact called the agent's model %d times, want 0", len(client.prompts))
	}

	if snap, _, _ := store.LoadSnapshot(context.Background()); snap == nil || snap.Summary != "compacted" {
		t.Fatalf("stored snapshot = %+v, want the compaction's", snap)
	}

	if _, err = a.Run(context.Background(), llm.Text("next")); err != nil {
		t.Fatal(err)
	}

	prompt := client.prompts[0]
	if prompt[1].TextContent() != memory.SummarySystemMessagePrefix+"compacted" || len(prompt) != 4 {
		t.Fatalf("next run's prompt = %+v, want system, the summary, the kept answer, the input", prompt)
	}
}

func TestCompact_NothingToCompact(t *testing.T) {
	a := mustNew(t, &scriptedLLM{}, "agent", "desc")
	a.SetMemory(simple.New(100))

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 0)

	info, err := a.Compact(context.Background())
	if err != nil || info.Compacted {
		t.Fatalf("Compact on an empty memory = %+v, %v; want nothing compacted and no error", info, err)
	}
}

func TestCompact_Refusals(t *testing.T) {
	compactions := 0

	noCompactor := mustNew(t, &scriptedLLM{}, "agent", "desc")
	noCompactor.SetMemory(simple.New(100))

	if _, err := noCompactor.Compact(context.Background()); !errors.Is(err, agent.ErrNoCompactor) {
		t.Fatalf("without a compactor: err = %v, want ErrNoCompactor", err)
	}

	plain := mustNew(t, &scriptedLLM{}, "agent", "desc")
	plain.SetMemory(&appendMemory{msgs: storedHistory()})
	plain.SetCompaction(keepLastCompactor(&compactions), 0)

	if _, err := plain.Compact(context.Background()); !errors.Is(err, memory.ErrSnapshotsUnsupported) {
		t.Fatalf("with a memory without snapshots: err = %v, want ErrSnapshotsUnsupported", err)
	}

	// A snapshot store set up without snapshot support.
	//nolint:staticcheck // the deprecated summarization is what has no snapshots
	summarizing := simple.New(100, simple.WithSummarization(&summaryLLM{}, 50, 10))
	if err := summarizing.Save(context.Background(), storedHistory()); err != nil {
		t.Fatal(err)
	}

	legacy := mustNew(t, &scriptedLLM{}, "agent", "desc")
	legacy.SetMemory(summarizing)
	legacy.SetCompaction(keepLastCompactor(&compactions), 0)

	info, err := legacy.Compact(context.Background())
	if !errors.Is(err, memory.ErrSnapshotsUnsupported) || info.Compacted || !errors.Is(info.Err, memory.ErrSnapshotsUnsupported) {
		t.Fatalf("with snapshots unsupported: %+v, %v; want ErrSnapshotsUnsupported and nothing compacted", info, err)
	}
}

// bigHistory is a stored history of n user/assistant turns of size chars each.
func bigHistory(n, size int) []llm.Message {
	text := strings.Repeat("x", size)

	msgs := make([]llm.Message, 0, 2*n)
	for range n {
		msgs = append(msgs, llm.UserMessage(llm.Text(text)), llm.AssistantMessage([]llm.ContentPart{llm.Text(text)}))
	}

	return msgs
}

func TestRun_CompactsBeforeTheFirstCallOnEstimate(t *testing.T) {
	for _, tc := range []struct {
		name        string
		history     []llm.Message
		compactions int
	}{
		// 10 turns of 2×4000 chars is about 20k tokens, over the 5000 threshold.
		{"large history", bigHistory(10, 4000), 1},
		{"small history", bigHistory(2, 100), 0},
	} {
		client := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) { return textResult("done"), nil }}

		a := mustNew(t, client, "agent", "desc")
		a.SetMemory(&appendMemory{msgs: tc.history})

		compactions := 0
		a.SetCompaction(keepLastCompactor(&compactions), 5000)

		if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}

		if compactions != tc.compactions {
			t.Errorf("%s: compactions = %d, want %d", tc.name, compactions, tc.compactions)
		}

		// The provider never saw the oversized prompt.
		if tc.compactions > 0 && !isSummary(client.prompts[0][1], "compacted") {
			t.Errorf("%s: first prompt = %+v, want the compacted session", tc.name, client.prompts[0])
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{
		llm.UserMessage(llm.Text(strings.Repeat("a", 400))),
		llm.AssistantMessage(nil, llm.ToolCall{ID: "c", Function: llm.FunctionCall{Name: "echo", Arguments: strings.Repeat("b", 40)}}),
		llm.ToolResultMessage("c", llm.Text(strings.Repeat("c", 80))),
	}

	// 400/4 + 40/4 + 80/4 = 130, plus an overhead per message.
	if got := agent.EstimateTokens(msgs); got < 130 || got > 160 {
		t.Fatalf("EstimateTokens = %d, want about 130 plus overhead", got)
	}
}

// twoRunsWithSnapshots runs an agent twice on store: the first run compacts
// (its first call reports 5000 tokens over a 1000 threshold), the second
// starts from what the first stored. It returns the second run's first prompt.
func twoRunsWithSnapshots(t *testing.T, store memory.Memory, checkpoint bool) []llm.Message {
	t.Helper()

	first := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		switch call {
		case 1, 2:
			return toolCallResultWithUsage("echo", fmt.Sprintf("c%d", call), paddedArgs, &llm.Usage{InputTokens: 5000}), nil
		default:
			return textResult("first done"), nil
		}
	}}

	a := mustNew(t, first, "agent", "desc")
	addEchoTool(t, a)
	a.SetMemory(store)
	a.SetCheckpoint(checkpoint)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 1000)

	if _, err := a.Run(context.Background(), llm.Text("task one")); err != nil {
		t.Fatal(err)
	}

	if compactions == 0 {
		t.Fatal("the first run did not compact")
	}

	second := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) { return textResult("second done"), nil }}

	b := mustNew(t, second, "agent", "desc")
	b.SetMemory(store)

	if _, err := b.Run(context.Background(), llm.Text("task two")); err != nil {
		t.Fatal(err)
	}

	return second.prompts[0]
}

func TestRun_SnapshotCarriesToTheNextRun(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		store := simple.New(100)
		prompt := twoRunsWithSnapshots(t, store, checkpoint)

		// system, the stored summary, then only the messages after it.
		if !isSummary(prompt[1], "compacted") {
			t.Fatalf("checkpoint %v: prompt = %+v, want the stored summary after the system prompt", checkpoint, prompt)
		}

		assertValidConversation(t, prompt)

		if last := prompt[len(prompt)-1]; last.TextContent() != "task two" {
			t.Fatalf("checkpoint %v: last message = %q", checkpoint, last.TextContent())
		}

		// The log holds every message once, and no summary.
		all := store.Len()
		seen := map[string]int{}

		snap, logged, err := store.LoadSnapshot(context.Background())
		if err != nil || snap == nil {
			t.Fatalf("checkpoint %v: LoadSnapshot = %v, %v", checkpoint, snap, err)
		}

		for _, lm := range logged {
			for _, tc := range lm.Message.ToolCalls {
				seen[tc.ID]++
			}

			if strings.HasPrefix(lm.Message.TextContent(), memory.SummarySystemMessagePrefix) {
				t.Fatalf("checkpoint %v: a summary was saved to the log", checkpoint)
			}
		}

		for id, n := range seen {
			if n > 1 {
				t.Fatalf("checkpoint %v: tool call %s logged %d times (log of %d)", checkpoint, id, n, all)
			}
		}
	}
}

// conflictingStore is a simple memory where another writer saves a snapshot
// just before the agent does.
type conflictingStore struct {
	*simple.Memory
}

func (c conflictingStore) SaveSnapshot(ctx context.Context, s memory.Snapshot, upTo memory.Cursor, prev *memory.Snapshot) error {
	_, logged, _ := c.LoadSnapshot(ctx)
	if err := c.Memory.SaveSnapshot(ctx, memory.Snapshot{Summary: "other writer"}, logged[0].Cursor, prev); err != nil {
		return err
	}

	return c.Memory.SaveSnapshot(ctx, s, upTo, prev)
}

func TestRun_SnapshotConflictKeepsTheRunGoing(t *testing.T) {
	store := conflictingStore{simple.New(100)}

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call == 1 {
			return toolCallResultWithUsage("echo", "c1", paddedArgs, &llm.Usage{InputTokens: 5000}), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)
	a.SetMemory(store)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 1000)

	if res, err := a.Run(context.Background(), llm.Text("go")); err != nil || res.TextContent() != "done" {
		t.Fatalf("Run = %v, %v; want the run to finish with its in-run summary", res, err)
	}

	if snap, _, _ := store.LoadSnapshot(context.Background()); snap == nil || snap.Summary != "other writer" {
		t.Fatalf("stored snapshot = %+v, want the other writer's kept", snap)
	}
}

func TestRun_SnapshotsUnsupportedFallsBack(t *testing.T) {
	//nolint:staticcheck // the deprecated summarization is what this test falls back from
	store := simple.New(100, simple.WithSummarization(&summaryLLM{}, 50, 10))
	if err := store.Save(context.Background(), []llm.Message{llm.UserMessage(llm.Text("old")), llm.AssistantMessage([]llm.ContentPart{llm.Text("ok")})}); err != nil {
		t.Fatal(err)
	}

	client := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) { return textResult("done"), nil }}

	a := mustNew(t, client, "agent", "desc")
	a.SetMemory(store)

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	if got := client.prompts[0][1].TextContent(); got != "old" {
		t.Fatalf("prompt = %+v, want the history retrieved as before", client.prompts[0])
	}
}

func TestRun_SnapshotsStoredAfterClippedToolResults(t *testing.T) {
	// Tool results longer than DefaultKeptToolResultChars are clipped in the
	// kept messages; later compactions of the run must still be stored.
	big := strings.Repeat("y", 20_000)

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call <= 3 {
			return toolCallResultWithUsage("big", fmt.Sprintf("c%d", call), paddedArgs, &llm.Usage{InputTokens: 5000}), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	if err := a.AddTool(mustTool(t, "big", func(context.Context, *struct{}) (string, error) { return big, nil })); err != nil {
		t.Fatal(err)
	}

	store := simple.New(100)
	a.SetMemory(store)
	a.SetCompaction(agent.NewSummaryCompactor(&summaryLLM{}, agent.WithKeepRecent(2), agent.WithKeepTokens(100_000)), 1000)

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	snap, logged, err := store.LoadSnapshot(context.Background())
	if err != nil || snap == nil {
		t.Fatalf("LoadSnapshot = %v, %v", snap, err)
	}

	// Three compactions ran; the last stored one keeps only the latest tool
	// turn and the answer after it.
	if len(logged) > 3 {
		t.Fatalf("stored snapshot leaves %d messages after it (up to %s), want the later compactions stored", len(logged), snap.UpTo)
	}
}

func TestRun_NoCompactionWhenOnlyTheSummaryIsLarge(t *testing.T) {
	// Every call reports 5000 tokens, over the threshold, but the messages
	// after the first compaction are small: compacting them again can't help.
	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call < 6 {
			return toolCallResultWithUsage("echo", fmt.Sprintf("c%d", call), "{}", &llm.Usage{InputTokens: 5000}), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	a.SetMemory(&appendMemory{msgs: bigHistory(10, 4000)})
	addEchoTool(t, a)

	compactions := 0
	a.SetCompaction(keepLastCompactor(&compactions), 5000)

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	// Only the large stored history, before the first call.
	if compactions != 1 {
		t.Fatalf("compactions = %d, want 1", compactions)
	}
}

func TestRun_RejectsKeptMessagesFromAnEmptyInput(t *testing.T) {
	ctx := context.Background()
	store := simple.New(100)

	if err := store.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("old")), llm.AssistantMessage([]llm.ContentPart{llm.Text("ok")})}); err != nil {
		t.Fatal(err)
	}

	_, logged, err := store.LoadSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if err = store.SaveSnapshot(ctx, memory.Snapshot{Summary: "s"}, logged[len(logged)-1].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	tooLong := &llm.ProviderError{Provider: "p", StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("prompt is too long")}
	client := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) { return nil, tooLong }}

	a := mustNew(t, client, "agent", "desc")
	a.SetMemory(store)
	a.SetCompaction(agent.CompactorFunc(func(context.Context, agent.CompactInput) (*agent.Compaction, error) {
		return &agent.Compaction{Kept: []llm.Message{llm.UserMessage(llm.Text("made up"))}}, nil
	}), 0)

	// No input, and the snapshot covers the whole log: nothing to keep.
	if _, err = a.Run(ctx); !errors.Is(err, agent.ErrInvalidCompaction) {
		t.Fatalf("err = %v, want ErrInvalidCompaction", err)
	}
}
