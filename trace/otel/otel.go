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

package otel

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/henomis/phero/trace"
)

// instrumentationName is the OpenTelemetry instrumentation scope name used when
// obtaining a tracer from the global provider.
const instrumentationName = "github.com/henomis/phero/trace/otel"

// maxAttrLen bounds the length of free-text span attributes (inputs, outputs,
// tool arguments) so spans stay a reasonable size.
const maxAttrLen = 1024

// Tracer is a trace.Tracer that records phero events as OpenTelemetry spans.
//
// It is safe for concurrent use. See the package documentation for the span
// model and correlation assumptions.
type Tracer struct {
	tracer oteltrace.Tracer

	mu    sync.Mutex
	roots map[string]rootSpan       // keyed by agent name
	llms  map[string]oteltrace.Span // keyed by agent name + iteration
	tools map[string]oteltrace.Span // keyed by agent name + tool call ID
}

// rootSpan holds an agent run's root span together with the context that
// carries it, so child spans can be parented correctly.
//
//nolint:containedctx // intentionally stores context to preserve span parentage
type rootSpan struct {
	ctx  context.Context
	span oteltrace.Span
}

// New returns a Tracer that records spans using the provided OpenTelemetry
// tracer. A nil tracer falls back to the global TracerProvider.
func New(tracer oteltrace.Tracer) *Tracer {
	if tracer == nil {
		tracer = otel.Tracer(instrumentationName)
	}

	return &Tracer{
		tracer: tracer,
		roots:  make(map[string]rootSpan),
		llms:   make(map[string]oteltrace.Span),
		tools:  make(map[string]oteltrace.Span),
	}
}

// NewDefault returns a Tracer backed by the global OpenTelemetry TracerProvider.
//
// The application is expected to have installed an SDK TracerProvider (with an
// exporter) via otel.SetTracerProvider before spans are recorded.
func NewDefault() *Tracer {
	return New(otel.Tracer(instrumentationName))
}

// Ensure Tracer implements trace.Tracer at compile time.
var _ trace.Tracer = (*Tracer)(nil)

// Trace records a single phero event as OpenTelemetry span activity.
func (t *Tracer) Trace(event trace.Event) {
	switch e := event.(type) {
	case trace.AgentStartEvent:
		t.startAgent(e)
	case trace.AgentEndEvent:
		t.endAgent(e)
	case trace.AgentRunSummaryEvent:
		t.finishAgent(e)
	case trace.LLMRequestEvent:
		t.startLLM(e)
	case trace.LLMResponseEvent:
		t.endLLM(e)
	case trace.ReasoningEvent:
		t.recordReasoning(e)
	case trace.ToolCallEvent:
		t.startTool(e)
	case trace.ToolResultEvent:
		t.endTool(e)
	}
}

func (t *Tracer) startAgent(e trace.AgentStartEvent) {
	ctx, span := t.tracer.Start(context.Background(), "agent "+e.AgentName)
	span.SetAttributes(
		attribute.String("phero.agent.name", e.AgentName),
		attribute.String("phero.agent.input", truncate(e.Input)),
	)
	t.mu.Lock()
	t.roots[e.AgentName] = rootSpan{ctx: ctx, span: span}
	t.mu.Unlock()
}

// endAgent records the run outcome on the root span. The span itself is ended
// by finishAgent on the subsequent run-summary event, which is emitted last.
func (t *Tracer) endAgent(e trace.AgentEndEvent) {
	t.mu.Lock()
	root, ok := t.roots[e.AgentName]
	t.mu.Unlock()

	if !ok {
		return
	}

	root.span.SetAttributes(attribute.Int("phero.agent.iterations", e.Iterations))

	if e.Err != nil {
		root.span.RecordError(e.Err)
		root.span.SetStatus(codes.Error, e.Err.Error())
	} else {
		root.span.SetAttributes(attribute.String("phero.agent.output", truncate(e.Output)))
		root.span.SetStatus(codes.Ok, "")
	}
}

// finishAgent attaches the aggregated run summary and ends the root span.
func (t *Tracer) finishAgent(e trace.AgentRunSummaryEvent) {
	t.mu.Lock()
	root, ok := t.roots[e.Summary.AgentName]
	delete(t.roots, e.Summary.AgentName)
	t.mu.Unlock()

	if !ok {
		return
	}

	s := e.Summary
	root.span.SetAttributes(
		attribute.Int("phero.run.llm_calls", s.LLMCalls),
		attribute.Int("phero.run.tool_calls", s.ToolCalls),
		attribute.Int("phero.run.tool_errors", s.ToolErrors),
		attribute.Int("gen_ai.usage.input_tokens", s.Usage.InputTokens),
		attribute.Int("gen_ai.usage.output_tokens", s.Usage.OutputTokens),
		attribute.Float64("phero.run.cost_usd", s.Usage.CostUSD),
		attribute.Int64("phero.run.latency_ms", s.Latency.Total.Milliseconds()),
	)
	root.span.End()
}

func (t *Tracer) startLLM(e trace.LLMRequestEvent) {
	parent := t.parentCtx(e.AgentName)
	_, span := t.tracer.Start(parent, "llm.request")
	span.SetAttributes(
		attribute.Int("phero.llm.message_count", e.MessageCount),
		attribute.Int("phero.agent.iteration", e.Iteration),
	)

	if len(e.ToolNames) > 0 {
		span.SetAttributes(attribute.StringSlice("phero.llm.tools", e.ToolNames))
	}

	t.mu.Lock()
	t.llms[llmKey(e.AgentName, e.Iteration)] = span
	t.mu.Unlock()
}

func (t *Tracer) endLLM(e trace.LLMResponseEvent) {
	key := llmKey(e.AgentName, e.Iteration)

	t.mu.Lock()
	span, ok := t.llms[key]
	delete(t.llms, key)
	t.mu.Unlock()

	if !ok {
		return
	}

	if e.Model != "" {
		span.SetAttributes(attribute.String("gen_ai.response.model", e.Model))
	}

	if e.Usage != nil {
		span.SetAttributes(
			attribute.Int("gen_ai.usage.input_tokens", e.Usage.InputTokens),
			attribute.Int("gen_ai.usage.output_tokens", e.Usage.OutputTokens),
		)

		if e.Usage.CacheReadTokens > 0 {
			span.SetAttributes(attribute.Int("phero.llm.cache_read_tokens", e.Usage.CacheReadTokens))
		}

		if e.Usage.CacheWriteTokens > 0 {
			span.SetAttributes(attribute.Int("phero.llm.cache_write_tokens", e.Usage.CacheWriteTokens))
		}
	}

	if e.Message != nil {
		span.SetAttributes(attribute.Int("phero.llm.tool_calls", len(e.Message.ToolCalls)))
	}

	span.End()
}

// recordReasoning attaches the model's extended-thinking text as an event on the
// agent's root span. The LLM span for the iteration has already ended by the
// time this event arrives.
func (t *Tracer) recordReasoning(e trace.ReasoningEvent) {
	t.mu.Lock()
	root, ok := t.roots[e.AgentName]
	t.mu.Unlock()

	if !ok {
		return
	}

	root.span.AddEvent("reasoning", oteltrace.WithAttributes(
		attribute.Int("phero.agent.iteration", e.Iteration),
		attribute.String("phero.llm.reasoning", truncate(e.Content)),
	))
}

func (t *Tracer) startTool(e trace.ToolCallEvent) {
	parent := t.parentCtx(e.AgentName)
	_, span := t.tracer.Start(parent, "tool "+e.ToolName)
	span.SetAttributes(
		attribute.String("phero.tool.name", e.ToolName),
		attribute.String("phero.tool.arguments", truncate(e.Arguments)),
		attribute.Int("phero.agent.iteration", e.Iteration),
	)
	t.mu.Lock()
	t.tools[toolKey(e.AgentName, e.CallID)] = span
	t.mu.Unlock()
}

func (t *Tracer) endTool(e trace.ToolResultEvent) {
	key := toolKey(e.AgentName, e.CallID)

	t.mu.Lock()
	span, ok := t.tools[key]
	delete(t.tools, key)
	t.mu.Unlock()

	if !ok {
		return
	}

	if e.Err != nil {
		span.RecordError(e.Err)
		span.SetStatus(codes.Error, e.Err.Error())
	} else {
		span.SetAttributes(attribute.String("phero.tool.result", truncate(e.Result)))
		span.SetStatus(codes.Ok, "")
	}

	span.End()
}

// parentCtx returns the context carrying the agent's root span, or a background
// context when no root span is active.
func (t *Tracer) parentCtx(agentName string) context.Context {
	t.mu.Lock()
	defer t.mu.Unlock()

	if root, ok := t.roots[agentName]; ok {
		return root.ctx
	}

	return context.Background()
}

func llmKey(agentName string, iteration int) string {
	return fmt.Sprintf("%s#%d", agentName, iteration)
}

func toolKey(agentName, callID string) string {
	return agentName + "#" + callID
}

// truncate shortens s to at most maxAttrLen runes, appending an ellipsis when
// it was longer.
func truncate(s string) string {
	runes := []rune(s)
	if len(runes) <= maxAttrLen {
		return s
	}

	return string(runes[:maxAttrLen]) + "…"
}
