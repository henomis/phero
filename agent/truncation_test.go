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
	"sync/atomic"
	"testing"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
)

// truncatedThenAnswer returns a tool call cut off at the output limit on the
// first call and a final answer on the next, recording each call's messages.
func truncatedThenAnswer(seen *[][]llm.Message) llm.LLM {
	return llm.Func(func(_ context.Context, msgs []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
		*seen = append(*seen, msgs)

		if len(*seen) == 1 {
			res := multiToolCallResult(toolCall("write", "c1", `{"content":"pack`))
			res.FinishReason = llm.FinishLength

			return res, nil
		}

		return &llm.Result{Message: &llm.Message{
			Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("done")},
		}, FinishReason: llm.FinishStop}, nil
	})
}

// newTruncationAgent returns an agent with a write tool that counts its calls.
func newTruncationAgent(t *testing.T, client llm.LLM, ran *atomic.Int32) *agent.Agent {
	t.Helper()

	a, err := agent.New(client, "coder", "You write files.")
	if err != nil {
		t.Fatal(err)
	}

	if err = a.AddTool(mustTool(t, "write", func(context.Context, *struct{}) (string, error) {
		ran.Add(1)
		return "written", nil
	})); err != nil {
		t.Fatal(err)
	}

	return a
}

// checkTruncatedRetry checks that the truncated call was not run and that the
// second LLM call got the error result for it.
func checkTruncatedRetry(t *testing.T, seen [][]llm.Message, ran int32) {
	t.Helper()

	if ran != 0 {
		t.Fatalf("write ran %d times, want 0", ran)
	}

	if len(seen) != 2 {
		t.Fatalf("LLM calls = %d, want 2", len(seen))
	}

	last := seen[1][len(seen[1])-1]
	if last.Role != llm.RoleTool || last.ToolCallID != "c1" || !last.ToolError {
		t.Fatalf("last message = %+v, want an error result for c1", last)
	}

	if got := llm.TextContent(last.Parts...); got != agent.TruncatedToolCallResult {
		t.Fatalf("result = %q, want %q", got, agent.TruncatedToolCallResult)
	}
}

func TestRun_TruncatedToolCallNotRun(t *testing.T) {
	var (
		seen [][]llm.Message
		ran  atomic.Int32
	)

	a := newTruncationAgent(t, truncatedThenAnswer(&seen), &ran)

	res, err := a.Run(context.Background(), llm.Text("write main.go"))
	if err != nil {
		t.Fatal(err)
	}

	checkTruncatedRetry(t, seen, ran.Load())

	if res.TextContent() != "done" || res.FinishReason != llm.FinishStop {
		t.Fatalf("result = %q (%s), want \"done\" (stop)", res.TextContent(), res.FinishReason)
	}
}

func TestRunStream_TruncatedToolCallNotRun(t *testing.T) {
	var (
		seen [][]llm.Message
		ran  atomic.Int32
	)

	a := newTruncationAgent(t, truncatedThenAnswer(&seen), &ran)

	var calls, results int

	for ev, err := range a.RunStream(context.Background(), llm.Text("write main.go")) {
		if err != nil {
			t.Fatal(err)
		}

		switch ev.Type {
		case agent.EventToolCall:
			calls++
		case agent.EventToolResult:
			results++

			if !ev.ToolError || ev.ToolCallID != "c1" {
				t.Fatalf("tool result event = %+v, want an error for c1", ev)
			}
		case agent.EventTextDelta, agent.EventReasoningDelta, agent.EventUsage, agent.EventDone:
		}
	}

	checkTruncatedRetry(t, seen, ran.Load())

	if calls != 1 || results != 1 {
		t.Fatalf("tool call/result events = %d/%d, want 1/1", calls, results)
	}
}

func TestRun_FinishReasonOnResult(t *testing.T) {
	client := llm.Func(func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
		return &llm.Result{Message: &llm.Message{
			Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("Once upon a")},
		}, FinishReason: llm.FinishLength}, nil
	})

	a, err := agent.New(client, "writer", "You write stories.")
	if err != nil {
		t.Fatal(err)
	}

	// A cut answer without tool calls is still returned as final.
	res, err := a.Run(context.Background(), llm.Text("tell a story"))
	if err != nil {
		t.Fatal(err)
	}

	if res.TextContent() != "Once upon a" || res.FinishReason != llm.FinishLength {
		t.Fatalf("result = %q (%s), want the cut text with FinishLength", res.TextContent(), res.FinishReason)
	}
}
