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
	"iter"
	"strings"
	"testing"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// streamingStub implements llm.StreamingLLM, emitting the configured text deltas
// then a terminal chunk with the assembled message.
type streamingStub struct {
	deltas []string
}

func (s *streamingStub) Execute(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
	msg := &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text(strings.Join(s.deltas, ""))}}
	return &llm.Result{Message: msg, Model: "test"}, nil
}

func (s *streamingStub) ExecuteStream(_ context.Context, _ []llm.Message, _ []*llm.Tool) iter.Seq2[llm.StreamChunk, error] {
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
			if ev.ToolName == "echo_tool" {
				sawToolCall = true
			}
		case agent.EventToolResult:
			if ev.ToolName == "echo_tool" && ev.ToolResult == "echoed" && !ev.ToolError {
				sawToolResult = true
			}
		case agent.EventDone:
			done = ev.Result
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
