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
	"sync"
	"testing"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/tracetest"
)

func newTracedProvider() (*tracetest.Recorder, *trace.Provider) {
	rec := tracetest.NewRecorder()
	return rec, trace.NewProvider(trace.WithProcessor(rec))
}

func runPayload(t *testing.T, d *trace.SpanData) *trace.Run {
	t.Helper()

	run, ok := d.Payload.(*trace.Run)
	if !ok {
		t.Fatalf("payload is %T, want *trace.Run", d.Payload)
	}

	return run
}

func TestTracing_ConcurrentRunsOfSameAgentAreSeparateTraces(t *testing.T) {
	client := llm.Func(func(_ context.Context, _ []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
		return textResult("hi"), nil
	})

	rec, p := newTracedProvider()
	a := mustNew(t, client, "agent", "desc")
	a.SetTracing(p)

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := a.Run(context.Background(), llm.Text("x")); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	runs := rec.ByKind(trace.KindRun)
	if len(runs) != 4 {
		t.Fatalf("expected 4 run spans, got %d", len(runs))
	}

	traces := map[trace.TraceID]bool{}
	for _, r := range runs {
		traces[r.Context.TraceID] = true

		children := rec.Children(r.Context)
		if len(children) != 1 || children[0].Kind != trace.KindLLM {
			t.Fatalf("each run should have exactly its own LLM call, got %d children", len(children))
		}
	}

	if len(traces) != 4 {
		t.Fatalf("expected 4 distinct traces, got %d", len(traces))
	}
}

func TestTracing_SubAgentNestsUnderToolAndRollsUp(t *testing.T) {
	childLLM := llm.Func(func(_ context.Context, _ []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
		r := textResult("child answer")
		r.Model = "child-model"

		return r, nil
	})
	child := mustNew(t, childLLM, "child", "child desc")

	childTool, err := child.AsTool("ask_child", "asks the child")
	if err != nil {
		t.Fatal(err)
	}

	parentLLM := makeStub(
		toolCallResultWithUsage("ask_child", "c1", `{"input":"q"}`, &llm.Usage{InputTokens: 1, OutputTokens: 1}), nil,
		textResult("done"), nil,
	)

	rec, p := newTracedProvider()
	parent := mustNew(t, parentLLM, "parent", "parent desc")
	parent.SetTracing(p)

	if err = parent.AddTool(childTool); err != nil {
		t.Fatal(err)
	}

	res, err := parent.Run(context.Background(), llm.Text("go"))
	if err != nil {
		t.Fatal(err)
	}

	tools := rec.ByKind(trace.KindTool)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool span, got %d", len(tools))
	}

	var childRun *trace.SpanData

	for _, r := range rec.ByKind(trace.KindRun) {
		if runPayload(t, r).Agent == "child" {
			childRun = r
		}
	}

	if childRun == nil || childRun.Parent.SpanID != tools[0].Context.SpanID {
		t.Fatalf("child run should nest under the tool span: %+v", childRun)
	}

	s := res.Summary
	if s.LLMCalls != 3 || s.ByAgent["child"].LLMCalls != 1 || s.ByAgent["parent"].LLMCalls != 2 {
		t.Fatalf("sub-agent work not rolled up: %+v", s)
	}

	if s.ByModel["child-model"].Calls != 1 {
		t.Fatalf("expected usage by model, got %+v", s.ByModel)
	}
}

func TestTracing_LLMCallStoresMessageDelta(t *testing.T) {
	stub := makeStub(toolCallResult("echo", "c1", "{}"), nil, textResult("done"), nil)
	rec, p := newTracedProvider()
	a := mustNew(t, stub, "agent", "desc")
	a.SetTracing(p)

	if err := a.AddTool(mustTool(t, "echo", func(context.Context, *struct{}) (string, error) { return "ok", nil })); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	calls := rec.ByKind(trace.KindLLM)
	if len(calls) != 2 {
		t.Fatalf("expected 2 LLM spans, got %d", len(calls))
	}

	first, _ := calls[0].Payload.(*trace.LLMCall)
	second, _ := calls[1].Payload.(*trace.LLMCall)

	// system + user, then assistant tool call + tool result.
	if first.MessageOffset != 0 || len(first.Messages) != 2 {
		t.Fatalf("first call: offset=%d messages=%d", first.MessageOffset, len(first.Messages))
	}

	if second.MessageOffset != 2 || len(second.Messages) != 2 || second.Messages[1].Role != llm.RoleTool {
		t.Fatalf("second call: offset=%d messages=%+v", second.MessageOffset, second.Messages)
	}

	if len(first.Tools) != 1 || first.Tools[0].Name != "echo" || first.Response == nil {
		t.Fatalf("missing tools or response: %+v", first)
	}
}

func TestTracing_StopReasons(t *testing.T) {
	tests := []struct {
		name   string
		cancel bool
		setup  func(a *agent.Agent)
		llm    llm.LLM
		want   trace.StopReason
		iters  int
	}{
		{
			name: "final", llm: makeStub(textResult("ok"), nil),
			want: trace.StopFinal, iters: 1,
		},
		{
			name: "max_iterations", llm: makeStub(toolCallResult("echo", "c", "{}"), nil),
			setup: func(a *agent.Agent) { a.SetMaxIterations(2) },
			want:  trace.StopMaxIterations, iters: 2,
		},
		{
			name: "guardrail",
			llm:  makeStub(nil, fmt.Errorf("blocked: %w", llm.ErrGuardrailBlocked)),
			want: trace.StopGuardrail, iters: 1,
		},
		{
			name: "error", llm: makeStub(nil, errors.New("boom")),
			want: trace.StopError, iters: 1,
		},
		{
			name: "memory error", llm: makeStub(textResult("ok"), nil),
			setup: func(a *agent.Agent) { a.SetMemory(&stubMemory{retrieveErr: errors.New("down")}) },
			want:  trace.StopError, iters: 0,
		},
		{
			name: "cancelled", cancel: true, llm: makeStub(textResult("ok"), nil),
			want: trace.StopCancelled, iters: 0,
		},
		{
			name: "stopped", llm: makeStub(toolCallResult("stop", "c", "{}"), nil, textResult("unreachable"), nil),
			want: trace.StopRequested, iters: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, p := newTracedProvider()
			a := mustNew(t, tt.llm, "agent", "desc")
			a.SetTracing(p)

			if err := a.AddTool(mustTool(t, "echo", func(context.Context, *struct{}) (string, error) { return "", nil })); err != nil {
				t.Fatal(err)
			}

			stop := func(context.Context, *struct{}) (string, error) { return "", agent.StopRun(errors.New("ask a human")) }
			if err := a.AddTool(mustTool(t, "stop", stop)); err != nil {
				t.Fatal(err)
			}

			if tt.setup != nil {
				tt.setup(a)
			}

			ctx, cancel := context.WithCancel(context.Background())
			if tt.cancel {
				cancel()
			}

			res, err := a.Run(ctx, llm.Text("go"))

			cancel()

			runs := rec.ByKind(trace.KindRun)
			if len(runs) != 1 {
				t.Fatalf("expected 1 run span, got %d", len(runs))
			}

			run := runPayload(t, runs[0])
			if run.StopReason != tt.want || run.Iterations != tt.iters {
				t.Fatalf("stop=%q iterations=%d, want %q %d", run.StopReason, run.Iterations, tt.want, tt.iters)
			}

			// Every run, failed ones included, returns its Summary.
			if res == nil || res.Summary == nil || res.Summary.StopReason != tt.want ||
				res.Summary.Iterations != tt.iters {
				t.Fatalf("result = %+v (err %v), want a Summary with stop %q", res, err, tt.want)
			}

			// Only a failure marks the run span as failed.
			if failed := runs[0].Err != nil; failed != (err != nil && tt.want != trace.StopRequested) {
				t.Fatalf("run span err = %v, run err = %v", runs[0].Err, err)
			}
		})
	}
}

func TestTracing_HandoffSpan(t *testing.T) {
	target := mustNew(t, makeStub(textResult("x"), nil), "writer", "writes")
	stub := makeStub(toolCallResult("handoff_to_writer", "h1", `{"context":"draft"}`), nil)

	rec, p := newTracedProvider()
	a := mustNew(t, stub, "router", "routes")
	a.SetTracing(p)

	if err := a.AddHandoff(target); err != nil {
		t.Fatal(err)
	}

	res, err := a.Run(context.Background(), llm.Text("go"))
	if err != nil {
		t.Fatal(err)
	}

	handoffs := rec.ByKind(trace.KindHandoff)
	if len(handoffs) != 1 || res.Handoffs[0].Span != handoffs[0].Context {
		t.Fatalf("expected the handoff span to be referenced by the result: %+v", handoffs)
	}

	if res.Summary.StopReason != trace.StopHandoff || res.Summary.HandoffAgents[0] != "writer" {
		t.Fatalf("unexpected summary %+v", res.Summary)
	}

	// The caller links the handed-off run back to the handoff.
	ctx := trace.WithLinks(trace.WithProvider(context.Background(), p), res.Handoffs[0].Span)
	if _, err = res.Handoffs[0].Agent.Run(ctx, llm.Text(res.Handoffs[0].Context)); err != nil {
		t.Fatal(err)
	}

	for _, r := range rec.ByKind(trace.KindRun) {
		if runPayload(t, r).Agent == "writer" && (len(r.Links) != 1 || r.Links[0] != res.Handoffs[0].Span) {
			t.Fatalf("handed-off run not linked: %+v", r.Links)
		}
	}
}

func TestTracing_StreamRecordsTimeToFirstToken(t *testing.T) {
	rec, p := newTracedProvider()
	a := mustNew(t, &streamingStub{deltas: []string{"a", "b"}}, "agent", "desc")
	a.SetTracing(p)

	var done *agent.Result

	for ev, err := range a.RunStream(context.Background(), llm.Text("go")) {
		if err != nil {
			t.Fatal(err)
		}

		if ev.Type == agent.EventDone {
			done = ev.Result
		}
	}

	call, _ := rec.ByKind(trace.KindLLM)[0].Payload.(*trace.LLMCall)
	if !call.Streaming || call.FirstTokenAt.IsZero() {
		t.Fatalf("expected streaming call with first token time: %+v", call)
	}

	if done.Summary.Latency.TimeToFirstToken <= 0 {
		t.Fatalf("expected positive time to first token, got %v", done.Summary.Latency.TimeToFirstToken)
	}
}

func TestTracing_ProviderInheritedFromContext(t *testing.T) {
	rec, p := newTracedProvider()
	a := mustNew(t, makeStub(textResult("ok"), nil), "agent", "desc")

	ctx := trace.WithSession(trace.WithProvider(context.Background(), p), "s1")

	res, err := a.Run(ctx, llm.Text("go"))
	if err != nil {
		t.Fatal(err)
	}

	if len(rec.ByKind(trace.KindRun)) != 1 || res.Summary.SessionID != "s1" {
		t.Fatalf("provider or session not taken from ctx: %+v", res.Summary)
	}
}
