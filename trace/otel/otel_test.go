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

package otel_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	otelbackend "github.com/henomis/phero/v2/trace/otel"
)

type describedLLM struct {
	fn func(messages []llm.Message) *llm.Result
}

func (d describedLLM) Execute(_ context.Context, messages []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
	return d.fn(messages), nil
}

func (describedLLM) Provider() string { return "google" }
func (describedLLM) Model() string    { return "gemini-test" }

func answer(text string) *llm.Result {
	return &llm.Result{
		Message:      &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text(text)}},
		Usage:        &llm.Usage{InputTokens: 10, OutputTokens: 5, ReasoningTokens: 2},
		Model:        "gemini-test-001",
		FinishReason: llm.FinishStop,
	}
}

func toolCall(name, args string) *llm.Result {
	return &llm.Result{
		Message: &llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
			ID: "call-1", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: name, Arguments: args},
		}}},
		Usage:        &llm.Usage{InputTokens: 4, OutputTokens: 1},
		FinishReason: llm.FinishToolCalls,
	}
}

func setup(t *testing.T, withIDs bool, opts ...otelbackend.Option) (*trace.Provider, *tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()

	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	exp := otelbackend.New(tp, opts...)

	popts := []trace.Option{trace.WithProcessor(exp)}
	if withIDs {
		popts = append(popts, trace.WithIDSource(exp.IDSource()))
	}

	return trace.NewProvider(popts...), mem, tp
}

func mustAgent(t *testing.T, client llm.LLM, name string) *agent.Agent {
	t.Helper()

	a, err := agent.New(client, name, name+" instructions")
	if err != nil {
		t.Fatal(err)
	}

	return a
}

func attr(s tracetest.SpanStub, key attribute.Key) (attribute.Value, bool) {
	for _, kv := range s.Attributes {
		if kv.Key == key {
			return kv.Value, true
		}
	}

	return attribute.Value{}, false
}

func byName(spans tracetest.SpanStubs, prefix string) []tracetest.SpanStub {
	var out []tracetest.SpanStub

	for _, s := range spans {
		if strings.HasPrefix(s.Name, prefix) {
			out = append(out, s)
		}
	}

	return out
}

func TestExporter_ConcurrentRunsOfSameAgent(t *testing.T) {
	for _, withIDs := range []bool{true, false} {
		p, mem, _ := setup(t, withIDs)
		a := mustAgent(t, describedLLM{fn: func([]llm.Message) *llm.Result { return answer("hi") }}, "writer")
		a.SetTracing(p)

		var wg sync.WaitGroup
		for range 5 {
			wg.Go(func() { _, _ = a.Run(context.Background(), llm.Text("x")) })
		}

		wg.Wait()

		runs := byName(mem.GetSpans(), "invoke_agent writer")
		chats := byName(mem.GetSpans(), "chat gemini-test")

		if len(runs) != 5 || len(chats) != 5 {
			t.Fatalf("withIDs=%v: expected 5 runs and 5 chats, got %d and %d", withIDs, len(runs), len(chats))
		}

		parents := map[string]bool{}
		for _, r := range runs {
			parents[r.SpanContext.SpanID().String()] = true
		}

		seen := map[string]bool{}

		for _, c := range chats {
			pid := c.Parent.SpanID().String()
			if !parents[pid] || seen[pid] {
				t.Fatalf("withIDs=%v: chat span not uniquely parented to a run", withIDs)
			}

			seen[pid] = true
		}
	}
}

func TestExporter_SharesIDsAndHonoursCallerSpan(t *testing.T) {
	p, mem, tp := setup(t, true)
	a := mustAgent(t, describedLLM{fn: func([]llm.Message) *llm.Result { return answer("hi") }}, "writer")
	a.SetTracing(p)

	ctx, caller := tp.Tracer("app").Start(context.Background(), "http request")

	res, err := a.Run(ctx, llm.Text("x"))
	if err != nil {
		t.Fatal(err)
	}

	caller.End()

	run := byName(mem.GetSpans(), "invoke_agent")[0]
	if run.Parent.SpanID() != caller.SpanContext().SpanID() {
		t.Fatal("run span should be a child of the caller's span")
	}

	if run.SpanContext.SpanID().String() != res.Summary.SpanID || run.SpanContext.TraceID().String() != res.Summary.TraceID {
		t.Fatal("phero and OTel span IDs should match")
	}
}

func TestExporter_SubAgentNestsUnderTool(t *testing.T) {
	p, mem, _ := setup(t, true)

	child := mustAgent(t, describedLLM{fn: func([]llm.Message) *llm.Result { return answer("child") }}, "child")

	childTool, err := child.AsTool("ask_child", "asks")
	if err != nil {
		t.Fatal(err)
	}

	calls := 0
	parent := mustAgent(t, describedLLM{fn: func([]llm.Message) *llm.Result {
		calls++
		if calls == 1 {
			return toolCall("ask_child", `{"input":"q"}`)
		}

		return answer("done")
	}}, "parent")
	parent.SetTracing(p)

	if err = parent.AddTool(childTool); err != nil {
		t.Fatal(err)
	}

	if _, err = parent.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	tool := byName(mem.GetSpans(), "execute_tool ask_child")[0]
	childRun := byName(mem.GetSpans(), "invoke_agent child")[0]

	if childRun.Parent.SpanID() != tool.SpanContext.SpanID() {
		t.Fatal("child run should nest under the tool span")
	}

	parentRun := byName(mem.GetSpans(), "invoke_agent parent")[0]
	if v, _ := attr(parentRun, "phero.run.llm_calls"); v.AsInt64() != 3 {
		t.Fatalf("parent totals should include the child: llm_calls=%d", v.AsInt64())
	}
}

func TestExporter_SemanticConventions(t *testing.T) {
	p, mem, _ := setup(t, true)
	a := mustAgent(t, describedLLM{fn: func([]llm.Message) *llm.Result { return answer("hi") }}, "writer")
	a.SetTracing(p)

	_, _ = a.Run(trace.WithSession(context.Background(), "s-1"), llm.Text("secret prompt"))

	chat := byName(mem.GetSpans(), "chat gemini-test")[0]
	want := map[attribute.Key]any{
		"gen_ai.operation.name":                "chat",
		"gen_ai.provider.name":                 "gcp.gemini",
		"gen_ai.request.model":                 "gemini-test",
		"gen_ai.response.model":                "gemini-test-001",
		"gen_ai.usage.input_tokens":            int64(10),
		"gen_ai.usage.output_tokens":           int64(5),
		"gen_ai.usage.reasoning.output_tokens": int64(2),
		"gen_ai.agent.name":                    "writer",
		"gen_ai.conversation.id":               "s-1",
	}

	for k, v := range want {
		got, ok := attr(chat, k)
		if !ok || got.AsInterface() != v {
			t.Errorf("%s = %v, want %v", k, got.AsInterface(), v)
		}
	}

	if fr, _ := attr(chat, "gen_ai.response.finish_reasons"); len(fr.AsStringSlice()) != 1 || fr.AsStringSlice()[0] != "stop" {
		t.Errorf("finish_reasons = %v", fr.AsInterface())
	}

	for _, s := range mem.GetSpans() {
		for _, kv := range s.Attributes {
			if strings.Contains(kv.Value.String(), "secret prompt") || kv.Key == "gen_ai.input.messages" {
				t.Fatalf("content must be off by default, found %s on %s", kv.Key, s.Name)
			}
		}
	}
}

func TestExporter_ContentOptInWithFullPromptAndLimit(t *testing.T) {
	p, mem, _ := setup(t, true, otelbackend.WithContent(true), otelbackend.WithMaxAttrBytes(4096))

	calls := 0
	a := mustAgent(t, describedLLM{fn: func([]llm.Message) *llm.Result {
		calls++
		if calls == 1 {
			return toolCall("echo", `{"v":1}`)
		}

		return answer(strings.Repeat("x", 10000))
	}}, "writer")
	a.SetTracing(p)

	echo, err := llm.NewTool("echo", "echoes", func(_ context.Context, _ *struct{}) (string, error) { return "echoed", nil })
	if err != nil {
		t.Fatal(err)
	}

	if err = a.AddTool(echo); err != nil {
		t.Fatal(err)
	}

	if _, err = a.Run(context.Background(), llm.Text("hello")); err != nil {
		t.Fatal(err)
	}

	chats := byName(mem.GetSpans(), "chat")
	second := chats[1]

	in, ok := attr(second, "gen_ai.input.messages")
	if !ok || !strings.Contains(in.AsString(), "hello") || !strings.Contains(in.AsString(), "tool_call_response") {
		t.Fatalf("second call should carry the full prompt, got %q", in.AsString())
	}

	if sys, found := attr(second, "gen_ai.system_instructions"); !found || !strings.Contains(sys.AsString(), "writer instructions") {
		t.Fatalf("missing system instructions: %v", sys.AsInterface())
	}

	out, _ := attr(second, "gen_ai.output.messages")
	if len(out.AsString()) > 4096+len("…") {
		t.Fatalf("output messages not truncated: %d bytes", len(out.AsString()))
	}

	tool := byName(mem.GetSpans(), "execute_tool echo")[0]
	if args, _ := attr(tool, "gen_ai.tool.call.arguments"); args.AsString() != `{"v":1}` {
		t.Fatalf("tool arguments = %q", args.AsString())
	}
}

func TestExporter_ErrorStatus(t *testing.T) {
	p, mem, _ := setup(t, true)
	ctx := trace.WithProvider(context.Background(), p)

	_, s := trace.Start(ctx, "execute_tool t", &trace.ToolCall{Name: "t"})
	s.End(context.DeadlineExceeded)

	got := mem.GetSpans()[0]
	if got.Status.Code != codes.Error || len(got.Events) == 0 {
		t.Fatalf("expected error status and exception event, got %+v", got.Status)
	}
}
