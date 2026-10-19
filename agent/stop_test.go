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
	"sync/atomic"
	"testing"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

// TestStopRun covers a tool that stops the run: the round's other calls
// still run, no further LLM call is made, and the caller gets the partial
// result, its Summary and an error matching ErrStopRun and the cause, while
// neither the tool's nor the run's span is marked as failed.
func TestStopRun(t *testing.T) {
	var calls, echoed atomic.Int32

	client := ctxIgnoringLLM(&calls, toolCall("stop", "c1", "{}"), toolCall("echo", "c2", "{}"))
	a := mustNew(t, client, "agent", "desc")

	rec, p := newTracedProvider()
	a.SetTracing(p)

	for _, tl := range []*llm.Tool{
		mustTool(t, "stop", func(context.Context, *struct{}) (string, error) {
			return "", agent.StopRun(errAskedHuman)
		}),
		mustTool(t, "echo", func(context.Context, *struct{}) (string, error) {
			echoed.Add(1)
			return "echo", nil
		}),
	} {
		if err := a.AddTool(tl); err != nil {
			t.Fatal(err)
		}
	}

	res, err := a.Run(context.Background(), llm.Text("go"))
	if !errors.Is(err, agent.ErrStopRun) || !errors.Is(err, errAskedHuman) {
		t.Fatalf("err = %v, want ErrStopRun and the cause", err)
	}

	if calls.Load() != 1 || echoed.Load() != 1 {
		t.Fatalf("LLM calls = %d, echo calls = %d, want 1 and 1", calls.Load(), echoed.Load())
	}

	if res == nil || res.Summary == nil || res.Summary.StopReason != trace.StopRequested ||
		res.Summary.ToolCalls != 2 || res.Summary.ToolErrors != 0 || res.Summary.Error != "" {
		t.Fatalf("result = %+v", res)
	}

	for _, s := range rec.ByKind(trace.KindTool) {
		if s.Err != nil {
			t.Errorf("tool span %s failed: %v", s.Name, s.Err)
		}

		if tc, _ := s.Payload.(*trace.ToolCall); tc.Name == "stop" && !strings.HasPrefix(tc.Result, "The run was stopped:") {
			t.Errorf("stop tool result = %q", tc.Result)
		}
	}

	if runs := rec.ByKind(trace.KindRun); len(runs) != 1 || runs[0].Err != nil {
		t.Fatalf("run spans = %v", runs)
	}
}

// TestStopRunByCancelCause checks cancelling the run with a StopRun cause
// is reported as a requested stop.
func TestStopRunByCancelCause(t *testing.T) {
	var calls atomic.Int32

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	a := mustNew(t, ctxIgnoringLLM(&calls, toolCall("ask", "c1", "{}")), "agent", "desc")
	if err := a.AddTool(mustTool(t, "ask", func(context.Context, *struct{}) (string, error) {
		cancel(agent.StopRun(errAskedHuman))
		return "question sent", nil
	})); err != nil {
		t.Fatal(err)
	}

	res, err := a.Run(ctx, llm.Text("go"))
	if !errors.Is(err, agent.ErrStopRun) || !errors.Is(err, errAskedHuman) {
		t.Fatalf("err = %v", err)
	}

	if res == nil || res.Summary == nil || res.Summary.StopReason != trace.StopRequested {
		t.Fatalf("result = %+v", res)
	}
}

// TestRunStream_ErrorCarriesResult checks a failed stream yields the partial
// result, with its Summary, alongside the error.
func TestRunStream_ErrorCarriesResult(t *testing.T) {
	a := mustNew(t, makeStub(nil, errors.New("boom")), "agent", "desc")

	var (
		got    *agent.Result
		gotErr error
	)

	for ev, err := range a.RunStream(context.Background(), llm.Text("go")) {
		if err != nil {
			got, gotErr = ev.Result, err
		}
	}

	if gotErr == nil || got == nil || got.Summary == nil || got.Summary.StopReason != trace.StopError {
		t.Fatalf("err = %v, result = %+v", gotErr, got)
	}
}
