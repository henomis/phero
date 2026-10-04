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
	"sync/atomic"
	"testing"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
)

var errAskedHuman = errors.New("asked human")

// ctxIgnoringLLM always asks for the given tool calls and never looks at ctx,
// like a stub, a cache hit or a local function. It counts its calls.
func ctxIgnoringLLM(calls *atomic.Int32, toolCalls ...llm.ToolCall) llm.LLM {
	return llm.Func(func(_ context.Context, _ []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
		calls.Add(1)

		res := multiToolCallResult(toolCalls...)
		res.Usage = &llm.Usage{InputTokens: 10, OutputTokens: 5}

		return res, nil
	})
}

// askHumanTool cancels the run with errAskedHuman, as a tool that pauses for a
// human answer does.
func askHumanTool(t *testing.T, cancel context.CancelCauseFunc) *llm.Tool {
	t.Helper()

	return mustTool(t, "ask_human", func(_ context.Context, _ *struct{}) (string, error) {
		cancel(errAskedHuman)
		return "question sent", nil
	})
}

func TestNew_DefaultMaxIterations(t *testing.T) {
	var calls atomic.Int32

	a := mustNew(t, ctxIgnoringLLM(&calls, toolCall("missing_tool", "c1", "{}")), "agent", "desc")

	res, err := a.Run(context.Background(), llm.Text("go"))
	if !errors.Is(err, agent.ErrMaxIterationsReached) {
		t.Fatalf("err = %v, want ErrMaxIterationsReached", err)
	}

	if got := calls.Load(); got != agent.DefaultMaxIterations {
		t.Fatalf("LLM calls = %d, want %d", got, agent.DefaultMaxIterations)
	}

	// No assistant text was produced, yet the caller still gets the Summary.
	if res == nil || res.Summary == nil || res.Summary.Usage.InputTokens == 0 {
		t.Fatalf("result = %+v, want a non-nil result with usage", res)
	}
}

func TestSetMaxIterations_ZeroRemovesLimit(t *testing.T) {
	const want = agent.DefaultMaxIterations + 5

	var calls atomic.Int32

	client := llm.Func(func(_ context.Context, _ []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
		if calls.Add(1) == want {
			return textResult("done"), nil
		}

		return toolCallResult("missing_tool", "c1", "{}"), nil
	})

	a := mustNew(t, client, "agent", "desc")
	a.SetMaxIterations(0)

	res, err := a.Run(context.Background(), llm.Text("go"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.TextContent() != "done" || calls.Load() != want {
		t.Fatalf("text = %q after %d calls, want %q after %d", res.TextContent(), calls.Load(), "done", want)
	}
}

// TestRun_CancelFromTool stops a run from inside a tool with an LLM that ignores
// ctx: the loop must notice the cancellation itself.
func TestRun_CancelFromTool(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	var calls atomic.Int32

	mem := &stubMemory{}
	a := mustNew(t, ctxIgnoringLLM(&calls, toolCall("ask_human", "c1", "{}")), "agent", "desc")
	a.SetMemory(mem)

	if err := a.AddTool(askHumanTool(t, cancel)); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	res, err := a.Run(ctx, llm.Text("go"))

	if !errors.Is(err, context.Canceled) || !errors.Is(err, errAskedHuman) {
		t.Fatalf("err = %v, want context.Canceled wrapping errAskedHuman", err)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("LLM calls = %d, want 1", got)
	}

	if res == nil || res.Summary == nil || res.Summary.Usage.InputTokens == 0 {
		t.Fatalf("result = %+v, want a non-nil result with usage", res)
	}

	// The turn is saved despite the cancelled ctx.
	if len(mem.saved) == 0 || mem.saveCtxErr != nil {
		t.Fatalf("saved %d messages with ctx err %v, want the turn saved on a live ctx", len(mem.saved), mem.saveCtxErr)
	}
}

func TestRun_CancelWithoutCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int32

	a := mustNew(t, ctxIgnoringLLM(&calls, toolCall("stop", "c1", "{}")), "agent", "desc")

	stop := mustTool(t, "stop", func(_ context.Context, _ *struct{}) (string, error) {
		cancel()
		return "stopped", nil
	})
	if err := a.AddTool(stop); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	_, err := a.Run(ctx, llm.Text("go"))
	if !errors.Is(err, context.Canceled) || err.Error() != context.Canceled.Error() {
		t.Fatalf("err = %v, want plain context.Canceled", err)
	}
}

// TestRun_CancelDuringLLMCall covers an LLM that honours ctx: its failure on the
// cancelled ctx is reported as a cancelled run, with a result and Summary.
func TestRun_CancelDuringLLMCall(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	client := llm.Func(func(ctx context.Context, _ []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("provider: %w", err)
		}

		res := toolCallResult("ask_human", "c1", "{}")
		res.Message.Parts = []llm.ContentPart{llm.Text("Let me ask.")}
		res.Usage = &llm.Usage{InputTokens: 10, OutputTokens: 5}

		return res, nil
	})

	a := mustNew(t, client, "agent", "desc")
	if err := a.AddTool(askHumanTool(t, cancel)); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	res, err := a.Run(ctx, llm.Text("go"))
	if !errors.Is(err, errAskedHuman) {
		t.Fatalf("err = %v, want errAskedHuman", err)
	}

	if res == nil || res.Summary == nil || res.TextContent() != "Let me ask." {
		t.Fatalf("result = %+v, want the partial text and a Summary", res)
	}
}

// TestRun_CancelLeavesNoUnansweredToolCalls cancels while a sibling tool call is
// in flight, then replays the saved turn through a provider that rejects
// assistant tool calls without a matching tool result, as OpenAI and Anthropic do.
func TestRun_CancelLeavesNoUnansweredToolCalls(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	var calls atomic.Int32

	mem := &stubMemory{}
	a := mustNew(t,
		ctxIgnoringLLM(&calls, toolCall("ask_human", "c1", "{}"), toolCall("slow", "c2", "{}")),
		"agent", "desc",
	)
	a.SetMemory(mem)

	slow := mustTool(t, "slow", func(ctx context.Context, _ *struct{}) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})

	for _, tool := range []*llm.Tool{askHumanTool(t, cancel), slow} {
		if err := a.AddTool(tool); err != nil {
			t.Fatalf("AddTool: %v", err)
		}
	}

	if _, err := a.Run(ctx, llm.Text("go")); !errors.Is(err, errAskedHuman) {
		t.Fatalf("err = %v, want errAskedHuman", err)
	}

	strict := llm.Func(func(_ context.Context, msgs []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
		answered := map[string]bool{}

		for _, m := range msgs {
			if m.Role == llm.RoleTool {
				answered[m.ToolCallID] = true
			}
		}

		for _, m := range msgs {
			for _, tc := range m.ToolCalls {
				if !answered[tc.ID] {
					return nil, fmt.Errorf("tool_call_id %s did not have a response message", tc.ID)
				}
			}
		}

		return textResult("resumed"), nil
	})

	next := mustNew(t, strict, "agent", "desc")
	next.SetMemory(&stubMemory{retrieved: mem.saved})

	res, err := next.Run(context.Background(), llm.Text("the human answered"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	if res.TextContent() != "resumed" {
		t.Fatalf("replay text = %q, want %q", res.TextContent(), "resumed")
	}
}

func TestRunStream_CancelFromTool(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	var calls atomic.Int32

	a := mustNew(t, ctxIgnoringLLM(&calls, toolCall("ask_human", "c1", "{}")), "agent", "desc")
	if err := a.AddTool(askHumanTool(t, cancel)); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	var streamErr error

	for ev, err := range a.RunStream(ctx, llm.Text("go")) {
		if err != nil {
			streamErr = err
			break
		}

		if ev.Type == agent.EventDone {
			t.Fatal("got EventDone, want the cancellation error")
		}
	}

	if !errors.Is(streamErr, context.Canceled) || !errors.Is(streamErr, errAskedHuman) {
		t.Fatalf("stream err = %v, want context.Canceled wrapping errAskedHuman", streamErr)
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("LLM calls = %d, want 1", got)
	}
}
