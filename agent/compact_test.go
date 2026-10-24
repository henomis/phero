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

	out, err := c.Compact(context.Background(), toolSession(5))
	if err != nil {
		t.Fatal(err)
	}

	assertValidConversation(t, out)

	// summary, the task, and the last 3 messages moved past the tool result
	// at the split: [result d, assistant e, result e] -> [assistant e, result e].
	if len(out) != 4 {
		t.Fatalf("got %d messages, want 4: %+v", len(out), out)
	}

	if out[0].Role != llm.RoleSystem || out[0].TextContent() != memory.SummarySystemMessagePrefix+"summary" {
		t.Fatalf("first message = %+v, want the summary", out[0])
	}

	if out[1].TextContent() != "fix the failing test" {
		t.Fatalf("second message = %+v, want the task", out[1])
	}

	if len(sum.prompts) != 1 || !strings.Contains(sum.prompts[0], "tool call a: bash") {
		t.Fatalf("summary prompt does not list the tool calls: %v", sum.prompts)
	}
}

func TestSummaryCompactor_NothingToSummarize(t *testing.T) {
	sum := &summaryLLM{}
	msgs := toolSession(1)

	out, err := agent.NewSummaryCompactor(sum, agent.WithKeepRecent(10)).Compact(context.Background(), msgs)
	if err != nil || len(out) != len(msgs) || len(sum.prompts) != 0 {
		t.Fatalf("out = %d messages, err = %v, prompts = %d; want the input unchanged", len(out), err, len(sum.prompts))
	}
}

func TestSummaryCompactor_ErrorIsCompactionError(t *testing.T) {
	cause := errors.New("summary model down")

	_, err := agent.NewSummaryCompactor(&summaryLLM{err: cause}, agent.WithKeepRecent(1)).Compact(context.Background(), toolSession(3))

	var cErr *agent.CompactionError
	if !errors.As(err, &cErr) || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want a CompactionError wrapping the cause", err)
	}
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

// keepLastCompactor replaces everything but the last message with a marker
// and counts its calls.
func keepLastCompactor(calls *int) agent.Compactor {
	return agent.CompactorFunc(func(_ context.Context, msgs []llm.Message) ([]llm.Message, error) {
		*calls++

		split := memory.SplitPoint(msgs, len(msgs)-1)

		return append([]llm.Message{llm.SystemMessage("compacted")}, msgs[split:]...), nil
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
			return toolCallResultWithUsage("echo", fmt.Sprintf("c%d", call), "{}", &llm.Usage{InputTokens: 900, OutputTokens: 200}), nil
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

		if prompt[1].TextContent() != "compacted" {
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
			return toolCallResultWithUsage("echo", "c2", "{}", &llm.Usage{InputTokens: 5000}), nil
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
		return toolCallResultWithUsage("echo", "c", "{}", &llm.Usage{InputTokens: 5000}), nil
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
