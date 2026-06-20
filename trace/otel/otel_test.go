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
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/henomis/phero/llm"
	phtrace "github.com/henomis/phero/trace"
	otelbackend "github.com/henomis/phero/trace/otel"
)

func newTestTracer(t *testing.T) (*otelbackend.Tracer, *tracetest.InMemoryExporter) {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))

	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	return otelbackend.New(tp.Tracer("test")), exp
}

func spanByName(spans tracetest.SpanStubs, name string) (tracetest.SpanStub, bool) {
	for _, s := range spans {
		if s.Name == name {
			return s, true
		}
	}

	return tracetest.SpanStub{}, false
}

func attrValue(s tracetest.SpanStub, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}

	return attribute.Value{}, false
}

func TestTracer_RunProducesNestedSpans(t *testing.T) {
	tr, exp := newTestTracer(t)
	now := time.Now()

	tr.Trace(phtrace.AgentStartEvent{AgentName: "writer", Input: "hello", Timestamp: now})
	tr.Trace(phtrace.LLMRequestEvent{AgentName: "writer", MessageCount: 2, ToolNames: []string{"calc"}, Iteration: 1, Timestamp: now})

	msg := llm.AssistantMessage([]llm.ContentPart{llm.Text("hi")})
	tr.Trace(phtrace.LLMResponseEvent{
		AgentName: "writer",
		Message:   &msg,
		Usage:     &llm.Usage{InputTokens: 12, OutputTokens: 7},
		Model:     "gpt-test",
		Iteration: 1,
		Timestamp: now,
	})
	tr.Trace(phtrace.ToolCallEvent{AgentName: "writer", ToolName: "calc", Arguments: `{"a":1}`, CallID: "call_1", Iteration: 1, Timestamp: now})
	tr.Trace(phtrace.ToolResultEvent{AgentName: "writer", ToolName: "calc", Result: "2", CallID: "call_1", Iteration: 1, Timestamp: now})
	tr.Trace(phtrace.AgentEndEvent{AgentName: "writer", Output: "done", Iterations: 1, Timestamp: now})
	tr.Trace(phtrace.AgentRunSummaryEvent{Summary: phtrace.RunSummary{
		AgentName: "writer",
		LLMCalls:  1,
		ToolCalls: 1,
		Usage:     phtrace.UsageSummary{InputTokens: 12, OutputTokens: 7, CostUSD: 0.0003},
		Latency:   phtrace.LatencySummary{Total: 1500 * time.Millisecond},
	}, Timestamp: now})

	spans := exp.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans (agent, llm, tool), got %d: %v", len(spans), spans.Snapshots())
	}

	root, ok := spanByName(spans, "agent writer")
	if !ok {
		t.Fatal("missing root span 'agent writer'")
	}

	llmSpan, ok := spanByName(spans, "llm.request")
	if !ok {
		t.Fatal("missing 'llm.request' span")
	}

	toolSpan, ok := spanByName(spans, "tool calc")
	if !ok {
		t.Fatal("missing 'tool calc' span")
	}

	// Child spans must share the root's trace and parent to it.
	if llmSpan.SpanContext.TraceID() != root.SpanContext.TraceID() {
		t.Error("llm span not in root trace")
	}

	if llmSpan.Parent.SpanID() != root.SpanContext.SpanID() {
		t.Error("llm span not parented to root")
	}

	if toolSpan.Parent.SpanID() != root.SpanContext.SpanID() {
		t.Error("tool span not parented to root")
	}

	// Attribute spot checks.
	if v, found := attrValue(llmSpan, "gen_ai.response.model"); !found || v.AsString() != "gpt-test" {
		t.Errorf("llm span model attr = %v (ok=%v)", v.AsString(), found)
	}

	if v, found := attrValue(llmSpan, "gen_ai.usage.input_tokens"); !found || v.AsInt64() != 12 {
		t.Errorf("llm span input tokens = %v (ok=%v)", v.AsInt64(), found)
	}

	if v, found := attrValue(root, "phero.run.cost_usd"); !found || v.AsFloat64() != 0.0003 {
		t.Errorf("root cost attr = %v (ok=%v)", v.AsFloat64(), found)
	}

	if v, found := attrValue(toolSpan, "phero.tool.result"); !found || v.AsString() != "2" {
		t.Errorf("tool result attr = %v (ok=%v)", v.AsString(), found)
	}
}

func TestTracer_ToolErrorSetsStatus(t *testing.T) {
	tr, exp := newTestTracer(t)
	now := time.Now()

	tr.Trace(phtrace.AgentStartEvent{AgentName: "a", Timestamp: now})
	tr.Trace(phtrace.ToolCallEvent{AgentName: "a", ToolName: "boom", CallID: "c1", Iteration: 1, Timestamp: now})
	tr.Trace(phtrace.ToolResultEvent{AgentName: "a", ToolName: "boom", Err: errors.New("kaboom"), CallID: "c1", Iteration: 1, Timestamp: now})
	tr.Trace(phtrace.AgentEndEvent{AgentName: "a", Err: errors.New("failed"), Iterations: 1, Timestamp: now})
	tr.Trace(phtrace.AgentRunSummaryEvent{Summary: phtrace.RunSummary{AgentName: "a", Error: "failed"}, Timestamp: now})

	spans := exp.GetSpans()

	toolSpan, ok := spanByName(spans, "tool boom")
	if !ok {
		t.Fatal("missing tool span")
	}

	if toolSpan.Status.Code != codes.Error {
		t.Errorf("expected error status on tool span, got %v", toolSpan.Status.Code)
	}

	if len(toolSpan.Events) == 0 {
		t.Error("expected recorded error event on tool span")
	}

	root, ok := spanByName(spans, "agent a")
	if !ok {
		t.Fatal("missing root span")
	}

	if root.Status.Code != codes.Error {
		t.Errorf("expected error status on root span, got %v", root.Status.Code)
	}
}

func TestTracer_RootSpanEndedOnSummary(t *testing.T) {
	tr, exp := newTestTracer(t)
	now := time.Now()

	tr.Trace(phtrace.AgentStartEvent{AgentName: "x", Timestamp: now})
	tr.Trace(phtrace.AgentEndEvent{AgentName: "x", Output: "ok", Iterations: 1, Timestamp: now})

	// Before the summary event the root span must still be open (not exported).
	if got := len(exp.GetSpans()); got != 0 {
		t.Fatalf("root span ended too early: %d spans exported", got)
	}

	tr.Trace(phtrace.AgentRunSummaryEvent{Summary: phtrace.RunSummary{AgentName: "x"}, Timestamp: now})

	if got := len(exp.GetSpans()); got != 1 {
		t.Fatalf("expected root span exported after summary, got %d", got)
	}
}
