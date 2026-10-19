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
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/tracetest"
)

func llmCallSpan(ctx context.Context) {
	_, s := trace.Start(ctx, "chat", &trace.LLMCall{
		Messages: []llm.Message{{
			Role: llm.RoleUser,
			Parts: []llm.ContentPart{
				llm.Text("hello world"),
				{Type: llm.ContentTypeImageBase64, ImageBase64: "aGVsbG8=", MIMEType: "image/png"},
			},
		}},
		Response: &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("hi")}},
	})
	s.End(nil)
}

func TestContent_NoCapture(t *testing.T) {
	stripped := tracetest.NewRecorder()
	full := tracetest.NewRecorder()
	p := trace.NewProvider(
		trace.WithProcessor(trace.Content(trace.ContentPolicy{}, stripped)),
		trace.WithProcessor(full),
	)

	llmCallSpan(trace.WithProvider(context.Background(), p))

	got, ok := stripped.Spans()[0].Payload.(*trace.LLMCall)
	if !ok {
		t.Fatal("unexpected payload type")
	}

	if got.Messages != nil || got.Response != nil {
		t.Fatalf("content not stripped: %+v", got)
	}

	orig, ok := full.Spans()[0].Payload.(*trace.LLMCall)
	if !ok {
		t.Fatal("unexpected payload type")
	}

	if len(orig.Messages) != 1 || orig.Response == nil {
		t.Fatal("other processors must see the full payload")
	}
}

func TestContent_TruncateAndBinary(t *testing.T) {
	rec := tracetest.NewRecorder()
	p := trace.NewProvider(trace.WithProcessor(
		trace.Content(trace.ContentPolicy{Capture: true, MaxBytes: 5}, rec),
	))

	llmCallSpan(trace.WithProvider(context.Background(), p))

	got, ok := rec.Spans()[0].Payload.(*trace.LLMCall)
	if !ok {
		t.Fatal("unexpected payload type")
	}

	parts := got.Messages[0].Parts
	if parts[0].Text != "hello…" {
		t.Fatalf("expected truncated text, got %q", parts[0].Text)
	}

	if parts[1].Type != llm.ContentTypeText || parts[1].ImageBase64 != "" || !strings.Contains(parts[1].Text, "sha256:") {
		t.Fatalf("binary part not summarised: %+v", parts[1])
	}
}

func TestRedact_WorksOnCopy(t *testing.T) {
	redacted := tracetest.NewRecorder()
	full := tracetest.NewRecorder()
	p := trace.NewProvider(
		trace.WithProcessor(trace.Redact(func(d *trace.SpanData) {
			if c, ok := d.Payload.(*trace.LLMCall); ok {
				c.Messages[0].Parts[0].Text = "[redacted]"
			}
		}, redacted)),
		trace.WithProcessor(full),
	)

	llmCallSpan(trace.WithProvider(context.Background(), p))

	r, _ := redacted.Spans()[0].Payload.(*trace.LLMCall)
	f, _ := full.Spans()[0].Payload.(*trace.LLMCall)

	if r.Messages[0].Parts[0].Text != "[redacted]" || f.Messages[0].Parts[0].Text != "hello world" {
		t.Fatalf("redaction leaked: redacted=%q full=%q", r.Messages[0].Parts[0].Text, f.Messages[0].Parts[0].Text)
	}
}

func TestBatch_DeliversOnShutdown(t *testing.T) {
	rec := tracetest.NewRecorder()
	b := trace.Batch(rec)
	p := trace.NewProvider(trace.WithProcessor(b))
	ctx := trace.WithProvider(context.Background(), p)

	for range 10 {
		_, s := trace.Start(ctx, "x", nil)
		s.End(nil)
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := len(rec.Spans()); got != 10 || b.Dropped() != 0 {
		t.Fatalf("expected 10 spans, got %d (dropped %d)", got, b.Dropped())
	}
}
