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
