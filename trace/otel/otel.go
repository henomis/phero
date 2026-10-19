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
	"encoding/json"
	"sync"
	"time"
	"unicode/utf8"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

const instrumentationName = "github.com/henomis/phero/v2/trace/otel"

// DefaultMaxAttrBytes is the default size limit of a content attribute.
const DefaultMaxAttrBytes = 64 * 1024

// Exporter is a trace.Processor that exports phero spans as OpenTelemetry
// spans. It is safe for concurrent use.
type Exporter struct {
	tracer       oteltrace.Tracer
	capture      bool
	maxAttrBytes int
	content      trace.Processor

	// spans holds the live OpenTelemetry span of each phero span.
	spans sync.Map // trace.SpanID -> oteltrace.Span
	// prompts holds, per run span, the prompt of its latest model call, so
	// that each call can export its full input.
	prompts sync.Map // trace.SpanID -> []llm.Message
}

var _ trace.Processor = (*Exporter)(nil)

// Option configures an Exporter.
type Option func(*Exporter)

// WithContent enables recording of content attributes: system instructions,
// input and output messages, tool definitions, arguments and results, and
// retrieval queries and documents. Default false.
func WithContent(capture bool) Option {
	return func(e *Exporter) { e.capture = capture }
}

// WithMaxAttrBytes sets the size limit of each content attribute. Zero or a
// negative value means unlimited. Default DefaultMaxAttrBytes.
func WithMaxAttrBytes(n int) Option {
	return func(e *Exporter) { e.maxAttrBytes = n }
}

// New returns an Exporter that creates spans with tp. A nil tp uses the
// global TracerProvider.
func New(tp oteltrace.TracerProvider, opts ...Option) *Exporter {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}

	e := &Exporter{
		tracer:       tp.Tracer(instrumentationName),
		maxAttrBytes: DefaultMaxAttrBytes,
	}

	for _, opt := range opts {
		opt(e)
	}

	e.content = trace.Content(trace.ContentPolicy{Capture: e.capture}, sink{e})

	return e
}

// IDSource returns a trace.IDSource that starts the OpenTelemetry span when
// the phero span starts, so that both share identifiers.
func (e *Exporter) IDSource() trace.IDSource { return idSource{e} }

// OnStart starts the OpenTelemetry span when the IDSource has not already.
func (e *Exporter) OnStart(ctx context.Context, s *trace.SpanData) {
	if _, ok := e.spans.Load(s.Context.SpanID); ok {
		return
	}

	_, span := e.tracer.Start(e.parentContext(ctx, s.Parent), s.Name,
		oteltrace.WithTimestamp(s.Start), oteltrace.WithSpanKind(spanKind(s.Kind)))
	e.spans.Store(s.Context.SpanID, span)
}

// OnEnd finishes the OpenTelemetry span of s.
func (e *Exporter) OnEnd(s *trace.SpanData) {
	e.content.OnEnd(s)
}

// Shutdown does nothing; shut the OpenTelemetry TracerProvider down to flush.
func (*Exporter) Shutdown(context.Context) error { return nil }

// parentContext returns ctx carrying the OpenTelemetry parent of a span whose
// phero parent is parent. Without a phero parent, the OpenTelemetry span
// already in ctx (if any) is the parent.
func (e *Exporter) parentContext(ctx context.Context, parent trace.SpanContext) context.Context {
	if !parent.IsValid() {
		return ctx
	}

	if sp, ok := e.spans.Load(parent.SpanID); ok {
		if span, isSpan := sp.(oteltrace.Span); isSpan {
			return oteltrace.ContextWithSpan(ctx, span)
		}
	}

	return oteltrace.ContextWithSpanContext(ctx, toOTel(parent))
}

func (e *Exporter) end(s *trace.SpanData) {
	sp, ok := e.spans.LoadAndDelete(s.Context.SpanID)
	if !ok {
		return
	}

	span, ok := sp.(oteltrace.Span)
	if !ok {
		return
	}

	span.SetName(spanName(s))
	span.SetAttributes(e.attributes(s)...)

	for _, ev := range s.Events {
		span.AddEvent(ev.Name, oteltrace.WithTimestamp(ev.Time), oteltrace.WithAttributes(convertAttrs(ev.Attrs)...))
	}

	for _, l := range s.Links {
		span.AddLink(oteltrace.Link{SpanContext: toOTel(l)})
	}

	if s.Err != nil {
		span.RecordError(s.Err)
		span.SetStatus(codes.Error, s.Err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.End(oteltrace.WithTimestamp(s.End))

	if s.Kind == trace.KindRun {
		e.prompts.Delete(s.Context.SpanID)
	}
}

// fullPrompt returns the full prompt of a model call by joining the previous
// prompt of the same run with the call's message delta.
func (e *Exporter) fullPrompt(s *trace.SpanData, call *trace.LLMCall) []llm.Message {
	var prev []llm.Message
	if v, ok := e.prompts.Load(s.Parent.SpanID); ok {
		prev, _ = v.([]llm.Message)
	}

	offset := min(call.MessageOffset, len(prev))
	full := make([]llm.Message, 0, offset+len(call.Messages))
	full = append(full, prev[:offset]...)
	full = append(full, call.Messages...)

	e.prompts.Store(s.Parent.SpanID, full)

	return full
}

// jsonAttr marshals v as a string attribute truncated to the size limit.
func (e *Exporter) jsonAttr(key attribute.Key, v any) attribute.KeyValue {
	b, err := json.Marshal(v)
	if err != nil {
		return key.String("")
	}

	return e.strAttr(key, string(b))
}

// strAttr returns a string attribute truncated to the size limit.
func (e *Exporter) strAttr(key attribute.Key, s string) attribute.KeyValue {
	if e.maxAttrBytes > 0 && len(s) > e.maxAttrBytes {
		cut := e.maxAttrBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}

		s = s[:cut] + "…"
	}

	return key.String(s)
}

// sink receives spans after the content policy has been applied.
type sink struct{ e *Exporter }

func (sink) OnStart(context.Context, *trace.SpanData) {}
func (s sink) OnEnd(d *trace.SpanData)                { s.e.end(d) }
func (sink) Shutdown(context.Context) error           { return nil }

type idSource struct{ e *Exporter }

func (src idSource) NewSpanContext(
	ctx context.Context, parent trace.SpanContext, kind trace.Kind, name string, start time.Time,
) trace.SpanContext {
	_, span := src.e.tracer.Start(src.e.parentContext(ctx, parent), name,
		oteltrace.WithTimestamp(start), oteltrace.WithSpanKind(spanKind(kind)))

	sc := fromOTel(span.SpanContext())
	if !sc.IsValid() {
		return trace.SpanContext{}
	}

	src.e.spans.Store(sc.SpanID, span)

	return sc
}

func spanKind(k trace.Kind) oteltrace.SpanKind {
	switch k {
	case trace.KindLLM, trace.KindEmbedding:
		return oteltrace.SpanKindClient
	case trace.KindRun, trace.KindTool, trace.KindHandoff, trace.KindMemory,
		trace.KindRetrieval, trace.KindMiddleware, trace.KindCustom:
		return oteltrace.SpanKindInternal
	default:
		return oteltrace.SpanKindInternal
	}
}

func toOTel(sc trace.SpanContext) oteltrace.SpanContext {
	cfg := oteltrace.SpanContextConfig{
		TraceID: oteltrace.TraceID(sc.TraceID),
		SpanID:  oteltrace.SpanID(sc.SpanID),
		Remote:  sc.Remote,
	}

	if sc.Sampled {
		cfg.TraceFlags = oteltrace.FlagsSampled
	}

	if ts, err := oteltrace.ParseTraceState(sc.TraceState); err == nil {
		cfg.TraceState = ts
	}

	return oteltrace.NewSpanContext(cfg)
}

func fromOTel(sc oteltrace.SpanContext) trace.SpanContext {
	return trace.SpanContext{
		TraceID:    trace.TraceID(sc.TraceID()),
		SpanID:     trace.SpanID(sc.SpanID()),
		Sampled:    sc.IsSampled(),
		Remote:     sc.IsRemote(),
		TraceState: sc.TraceState().String(),
	}
}
