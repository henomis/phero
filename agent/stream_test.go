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
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
)

// streamingStub implements llm.StreamingLLM, emitting the configured text deltas
// then a terminal chunk with the assembled message.
type streamingStub struct {
	deltas []string
}

func (s *streamingStub) Execute(_ context.Context, _ []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
	msg := &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text(strings.Join(s.deltas, ""))}}
	return &llm.Result{Message: msg, Model: "test"}, nil
}

func (s *streamingStub) ExecuteStream(_ context.Context, _ []llm.Message, _ ...llm.CallOption) iter.Seq2[llm.StreamChunk, error] {
	return func(yield func(llm.StreamChunk, error) bool) {
		var full strings.Builder
		for _, d := range s.deltas {
			full.WriteString(d)

			if !yield(llm.StreamChunk{TextDelta: d}, nil) {
				return
			}
		}

		msg := &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text(full.String())}}
		yield(llm.StreamChunk{Done: true, Message: msg, Model: "test", Usage: &llm.Usage{InputTokens: 1, OutputTokens: 1}}, nil)
	}
}

func TestRunStream_StreamsTextDeltasAndDone(t *testing.T) {
	a := mustNew(t, &streamingStub{deltas: []string{"Hello", ", ", "world"}}, "agent", "desc")

	var (
		text    strings.Builder
		done    *agent.Result
		nEvents int
	)

	for ev, err := range a.RunStream(context.Background(), llm.Text("hi")) {
		if err != nil {
			t.Fatalf("RunStream: %v", err)
		}

		nEvents++

		switch ev.Type {
		case agent.EventTextDelta:
			text.WriteString(ev.TextDelta)
		case agent.EventDone:
			done = ev.Result
		case agent.EventReasoningDelta, agent.EventToolCall, agent.EventToolResult, agent.EventUsage,
			agent.EventCompactionStart, agent.EventCompactionEnd:
			// not checked in this test
		}
	}

	if text.String() != "Hello, world" {
		t.Fatalf("streamed text = %q, want %q", text.String(), "Hello, world")
	}

	if done == nil {
		t.Fatal("expected an EventDone with a result")
	}

	if done.TextContent() != "Hello, world" {
		t.Fatalf("done result text = %q, want %q", done.TextContent(), "Hello, world")
	}

	if nEvents < 4 { // 3 deltas + done
		t.Fatalf("expected at least 4 events, got %d", nEvents)
	}
}

func TestRunStream_EmitsToolEventsViaBufferedFallback(t *testing.T) {
	// stubLLM does not implement StreamingLLM, so RunStream falls back to buffered
	// responses while still emitting tool call/result events around execution.
	stub := &stubLLM{
		responses: []*llm.Result{
			toolCallResult("echo_tool", "c1", `{}`),
			textResult("done"),
		},
		errs: []error{nil, nil},
	}

	a := mustNew(t, stub, "agent", "desc")
	if err := a.AddTool(mustTool(t, "echo_tool", func(_ context.Context, _ *struct{}) (string, error) {
		return "echoed", nil
	})); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	var (
		sawToolCall   bool
		sawToolResult bool
		done          *agent.Result
	)

	for ev, err := range a.RunStream(context.Background(), llm.Text("go")) {
		if err != nil {
			t.Fatalf("RunStream: %v", err)
		}

		switch ev.Type {
		case agent.EventToolCall:
			if ev.ToolCallID == "c1" &&
				ev.ToolName == "echo_tool" &&
				ev.ToolArgs == `{}` {
				sawToolCall = true
			}
		case agent.EventToolResult:
			if ev.ToolCallID == "c1" &&
				ev.ToolName == "echo_tool" &&
				ev.ToolResult == "echoed" &&
				!ev.ToolError {
				sawToolResult = true
			}
		case agent.EventDone:
			done = ev.Result
		case agent.EventTextDelta, agent.EventReasoningDelta, agent.EventUsage,
			agent.EventCompactionStart, agent.EventCompactionEnd:
			// not checked in this test
		}
	}

	if !sawToolCall {
		t.Error("expected an EventToolCall for echo_tool")
	}

	if !sawToolResult {
		t.Error("expected an EventToolResult for echo_tool")
	}

	if done == nil || done.TextContent() != "done" {
		t.Fatalf("done result = %v, want text %q", done, "done")
	}
}

func TestRunStream_PropagatesError(t *testing.T) {
	stub := makeStub(nil, context.Canceled)
	a := mustNew(t, stub, "agent", "desc")

	var gotErr error

	for _, err := range a.RunStream(context.Background(), llm.Text("go")) {
		if err != nil {
			gotErr = err
		}
	}

	if gotErr == nil {
		t.Fatal("expected an error from RunStream")
	}
}

// TestRunStream_ToolResultsInCompletionOrder checks that a fast tool's result
// is emitted while a slow tool of the same round still runs: the slow tool
// only returns once the consumer has seen the fast result.
func TestRunStream_ToolResultsInCompletionOrder(t *testing.T) {
	two := toolCallResult("slow", "s", "{}")
	two.Message.ToolCalls = append(two.Message.ToolCalls, toolCallResult("fast", "f", "{}").Message.ToolCalls...)

	stub := &stubLLM{responses: []*llm.Result{two, textResult("done")}, errs: []error{nil, nil}}
	a := mustNew(t, stub, "agent", "desc")

	sawFast := make(chan struct{})

	for _, tl := range []*llm.Tool{
		mustTool(t, "slow", func(context.Context, *struct{}) (string, error) {
			select {
			case <-sawFast:
				return "slow", nil
			case <-time.After(5 * time.Second):
				return "", errors.New("the fast result was not emitted before the slow tool returned")
			}
		}),
		mustTool(t, "fast", func(context.Context, *struct{}) (string, error) { return "fast", nil }),
	} {
		if err := a.AddTool(tl); err != nil {
			t.Fatal(err)
		}
	}

	var order []string

	for ev, err := range a.RunStream(context.Background(), llm.Text("go")) {
		if err != nil {
			t.Fatalf("RunStream: %v", err)
		}

		if ev.Type == agent.EventToolResult {
			if ev.ToolError {
				t.Fatalf("%s failed: %s", ev.ToolName, ev.ToolResult)
			}

			order = append(order, ev.ToolName)
			if ev.ToolName == "fast" {
				close(sawFast)
			}
		}
	}

	if len(order) != 2 || order[0] != "fast" || order[1] != "slow" {
		t.Fatalf("tool results = %v, want fast then slow", order)
	}
}

func TestRunStream_EmitsUsagePerIteration(t *testing.T) {
	stub := &stubLLM{
		responses: []*llm.Result{
			toolCallResultWithUsage("echo_tool", "c1", "{}", &llm.Usage{InputTokens: 100, OutputTokens: 10}),
			textResult("done"),
		},
		errs: []error{nil, nil},
	}

	a := mustNew(t, stub, "agent", "desc")
	if err := a.AddTool(mustTool(t, "echo_tool", func(context.Context, *struct{}) (string, error) { return "ok", nil })); err != nil {
		t.Fatal(err)
	}

	var usage []int

	for ev, err := range a.RunStream(context.Background(), llm.Text("go")) {
		if err != nil {
			t.Fatalf("RunStream: %v", err)
		}

		if ev.Type == agent.EventUsage {
			usage = append(usage, ev.Iteration, ev.Usage.InputTokens)
		}
	}

	// textResult reports 10 input tokens.
	if len(usage) != 4 || usage[0] != 1 || usage[1] != 100 || usage[2] != 2 || usage[3] != 10 {
		t.Fatalf("usage events (iteration, input tokens) = %v", usage)
	}
}

// trackingTool records how many calls run at once and the order calls start.
type tracking struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	starts      []string
}

func (tr *tracking) tool(t *testing.T, name string) *llm.Tool {
	t.Helper()

	return mustTool(t, name, func(context.Context, *struct{}) (string, error) {
		tr.mu.Lock()
		tr.inFlight++
		tr.maxInFlight = max(tr.maxInFlight, tr.inFlight)
		tr.starts = append(tr.starts, name)
		tr.mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		tr.mu.Lock()
		tr.inFlight--
		tr.mu.Unlock()

		return name, nil
	})
}

// roundOf returns an LLM result calling the named tools in one round.
func roundOf(names ...string) *llm.Result {
	res := toolCallResult(names[0], "c0", "{}")
	for i, n := range names[1:] {
		res.Message.ToolCalls = append(res.Message.ToolCalls, toolCallResult(n, fmt.Sprintf("c%d", i+1), "{}").Message.ToolCalls...)
	}

	return res
}

func TestRun_MaxParallelTools(t *testing.T) {
	tr := &tracking{}
	stub := &stubLLM{responses: []*llm.Result{roundOf("a", "b", "c", "d"), textResult("done")}, errs: []error{nil, nil}}

	a := mustNew(t, stub, "agent", "desc")
	for _, n := range []string{"a", "b", "c", "d"} {
		if err := a.AddTool(tr.tool(t, n)); err != nil {
			t.Fatal(err)
		}
	}

	a.SetMaxParallelTools(2)

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	if tr.maxInFlight != 2 {
		t.Fatalf("max calls at once = %d, want 2", tr.maxInFlight)
	}
}

func TestRun_ExclusiveToolRunsAloneInOrder(t *testing.T) {
	tr := &tracking{}
	stub := &stubLLM{responses: []*llm.Result{roundOf("a", "b", "x", "c"), textResult("done")}, errs: []error{nil, nil}}

	a := mustNew(t, stub, "agent", "desc")
	for _, n := range []string{"a", "b", "c"} {
		if err := a.AddTool(tr.tool(t, n)); err != nil {
			t.Fatal(err)
		}
	}

	if err := a.AddTool(tr.tool(t, "x").SetExclusive(true)); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	// a and b may overlap; x starts third and alone; c starts last.
	if len(tr.starts) != 4 || tr.starts[2] != "x" || tr.starts[3] != "c" || tr.maxInFlight != 2 {
		t.Fatalf("starts = %v, max at once = %d; want x third and alone, c last", tr.starts, tr.maxInFlight)
	}
}
