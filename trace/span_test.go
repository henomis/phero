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

package trace_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/tracetest"
)

func newRecorded() (*tracetest.Recorder, context.Context) {
	rec := tracetest.NewRecorder()
	p := trace.NewProvider(trace.WithProcessor(rec))

	return rec, trace.WithProvider(context.Background(), p)
}

func TestTraceparent_RoundTrip(t *testing.T) {
	_, span := trace.Start(context.Background(), "root", nil)
	tp := span.Context().Traceparent()

	sc, err := trace.ParseTraceparent(tp)
	if err != nil {
		t.Fatalf("parse %q: %v", tp, err)
	}

	if sc.TraceID != span.Context().TraceID || sc.SpanID != span.Context().SpanID || !sc.Remote || !sc.Sampled {
		t.Fatalf("round trip mismatch: %+v vs %+v", sc, span.Context())
	}
}

func TestParseTraceparent_Invalid(t *testing.T) {
	for _, s := range []string{
		"",
		"00-00000000000000000000000000000000-0000000000000000-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra",
	} {
		if _, err := trace.ParseTraceparent(s); !errors.Is(err, trace.ErrInvalidTraceparent) {
			t.Errorf("%q: expected ErrInvalidTraceparent, got %v", s, err)
		}
	}
}

func TestExtract_ParentsNewSpan(t *testing.T) {
	const tp = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	rec, ctx := newRecorded()
	ctx = trace.Extract(ctx, tp, "vendor=x")

	if got := trace.Inject(ctx); got != tp {
		t.Fatalf("Inject before start = %q", got)
	}

	ctx, span := trace.Start(ctx, "child", nil)
	span.End(nil)

	d := rec.Spans()[0]
	if d.Parent.SpanID.String() != "00f067aa0ba902b7" || !d.Parent.Remote {
		t.Fatalf("unexpected parent %+v", d.Parent)
	}

	if d.Context.TraceID.String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id not inherited: %s", d.Context.TraceID)
	}

	if trace.InjectState(ctx) != "vendor=x" {
		t.Fatalf("tracestate not passed through: %q", trace.InjectState(ctx))
	}

	if !strings.Contains(trace.Inject(ctx), span.Context().SpanID.String()) {
		t.Fatal("Inject should return the current span")
	}
}

func TestExtract_InvalidKeepsContext(t *testing.T) {
	ctx := context.Background()
	if trace.Extract(ctx, "garbage") != ctx {
		t.Fatal("invalid traceparent should return ctx unchanged")
	}
}

func TestStart_NestsAndRollsUp(t *testing.T) {
	rec, ctx := newRecorded()

	ctx, run := trace.Start(ctx, "invoke_agent a", &trace.Run{Agent: "a"})

	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			tctx, tool := trace.Start(ctx, "execute_tool t", &trace.ToolCall{Name: "t"})
			_, call := trace.Start(tctx, "chat m", &trace.LLMCall{
				ResponseModel: "m", Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}, CostUSD: 0.5,
			})
			call.End(nil)
			tool.End(errors.New("boom"))
		})
	}

	wg.Wait()
	run.End(nil)

	if got := len(rec.Children(run.Context())); got != 3 {
		t.Fatalf("expected 3 tool children, got %d", got)
	}

	tot := run.Totals()
	if tot.LLMCalls != 3 || tot.ToolCalls != 3 || tot.ToolErrors != 3 || tot.Usage.InputTokens != 30 {
		t.Fatalf("unexpected totals %+v", tot)
	}

	if tot.ByModel["m"].Calls != 3 || tot.ByAgent["a"].LLMCalls != 3 || tot.ByTool["t"].Errors != 3 {
		t.Fatalf("unexpected breakdown %+v", tot)
	}

	summary := trace.NewRunSummary(run)
	if summary.AgentName != "a" || summary.ToolErrors != 3 || summary.Tools[0].Calls != 3 {
		t.Fatalf("unexpected summary %+v", summary)
	}
}

func TestEnd_Idempotent(t *testing.T) {
	rec, ctx := newRecorded()
	_, s := trace.Start(ctx, "x", nil)
	s.End(nil)
	s.End(errors.New("late"))

	spans := rec.Spans()
	if len(spans) != 1 || spans[0].Err != nil {
		t.Fatalf("expected a single successful span, got %+v", spans)
	}
}

func TestNilProvider_RecordsTotalsOnly(t *testing.T) {
	ctx, run := trace.Start(context.Background(), "r", &trace.Run{Agent: "a"})
	_, call := trace.Start(ctx, "c", &trace.LLMCall{Usage: llm.Usage{OutputTokens: 4}})
	call.End(nil)
	run.End(nil)

	if run.Totals().Usage.OutputTokens != 4 {
		t.Fatalf("totals not rolled up without a provider: %+v", run.Totals())
	}
}

func TestNilSpan_MethodsAreNoops(_ *testing.T) {
	var s *trace.Span
	s.SetAttributes(trace.String("k", "v"))
	s.AddEvent("e")
	s.End(nil)
	_ = s.Context()
	_ = trace.SpanFrom(context.Background()).Totals()
}

func TestAttributesSessionAndLinks(t *testing.T) {
	rec, ctx := newRecorded()
	ctx = trace.WithAttributes(ctx, trace.String("tenant", "acme"))
	ctx = trace.WithSession(ctx, "sess-1")

	_, linked := trace.Start(context.Background(), "other", nil)
	ctx = trace.WithLinks(ctx, linked.Context())

	ctx, parent := trace.Start(ctx, "parent", nil)
	_, child := trace.Start(ctx, "child", nil)
	child.End(nil)
	parent.End(nil)

	p := rec.ByName("parent")[0]
	c := rec.ByName("child")[0]

	if len(p.Links) != 1 || len(c.Links) != 0 {
		t.Fatalf("links should apply to the next span only: parent=%v child=%v", p.Links, c.Links)
	}

	if c.SessionID != "sess-1" || len(c.Attrs) != 1 || c.Attrs[0].Value != "acme" {
		t.Fatalf("context attributes not propagated: %+v", c)
	}
}

func TestPayloadFrom(t *testing.T) {
	ctx, _ := trace.Start(context.Background(), "chat", &trace.LLMCall{})
	ctx, _ = trace.Start(ctx, "retry", &trace.Middleware{Name: "retry"})

	call, ok := trace.PayloadFrom[*trace.LLMCall](ctx)
	if !ok || call == nil {
		t.Fatal("expected to find the enclosing LLMCall")
	}

	if _, found := trace.PayloadFrom[*trace.Run](ctx); found {
		t.Fatal("no Run in ctx")
	}
}

func TestLogAttrs(t *testing.T) {
	if trace.LogAttrs(context.Background()) != nil {
		t.Fatal("expected nil without a span")
	}

	ctx, s := trace.Start(context.Background(), "x", nil)

	attrs := trace.LogAttrs(ctx)
	if len(attrs) != 2 || attrs[0].Value.String() != s.Context().TraceID.String() {
		t.Fatalf("unexpected attrs %v", attrs)
	}
}

func BenchmarkStartEnd_NoProcessors(b *testing.B) {
	ctx, run := trace.Start(context.Background(), "r", &trace.Run{Agent: "a"})
	defer run.End(nil)

	b.ReportAllocs()

	for b.Loop() {
		_, s := trace.Start(ctx, "t", &trace.ToolCall{Name: "t"})
		s.End(nil)
	}
}

func TestInstrumentLLM(t *testing.T) {
	rec, ctx := newRecorded()
	inner := llm.Func(func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
		return &llm.Result{Message: &llm.Message{Role: llm.RoleAssistant}, Model: "m", Usage: &llm.Usage{OutputTokens: 3}}, nil
	})
	client := trace.InstrumentLLM(inner)

	if _, err := client.Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("hi"))}); err != nil {
		t.Fatal(err)
	}

	// Inside an existing LLM call span the wrapper adds nothing.
	callCtx, outer := trace.Start(ctx, "chat", &trace.LLMCall{})
	if _, err := client.Execute(callCtx, nil); err != nil {
		t.Fatal(err)
	}

	outer.End(nil)

	calls := rec.ByKind(trace.KindLLM)
	if len(calls) != 2 {
		t.Fatalf("expected 2 LLM spans (wrapped + outer), got %d", len(calls))
	}

	p, _ := calls[0].Payload.(*trace.LLMCall)
	if p.ResponseModel != "m" || p.Usage.OutputTokens != 3 || len(p.Messages) != 1 {
		t.Fatalf("unexpected payload %+v", p)
	}
}
