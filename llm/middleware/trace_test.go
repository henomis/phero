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

package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/tracetest"
)

func recordedCall(t *testing.T) (*tracetest.Recorder, context.Context, *trace.Span) {
	t.Helper()

	rec := tracetest.NewRecorder()
	ctx := trace.WithProvider(context.Background(), trace.NewProvider(trace.WithProcessor(rec)))
	ctx, span := trace.Start(ctx, "chat", &trace.LLMCall{})

	return rec, ctx, span
}

func TestRetry_HonoursRetryAfterAndTraces(t *testing.T) {
	calls := 0
	inner := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []llm.CallOption) (*llm.Result, error) {
		calls++
		if calls == 1 {
			return nil, &llm.ProviderError{Provider: "x", StatusCode: 429, RetryAfter: 30 * time.Millisecond, Err: errors.New("rate limited")}
		}

		return &llm.Result{Message: &llm.Message{}}, nil
	}}

	mw, err := NewRetry(3, WithInitialBackoff(0))
	if err != nil {
		t.Fatal(err)
	}

	rec, ctx, call := recordedCall(t)
	start := time.Now()

	if _, err = llm.Use(inner, mw).Execute(ctx, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("retry-after not honoured: slept %v", elapsed)
	}

	call.End(nil)

	spans := rec.ByKind(trace.KindMiddleware)
	if len(spans) != 1 || spans[0].Parent.SpanID != call.Context().SpanID {
		t.Fatalf("expected one retry span under the call, got %+v", spans)
	}

	p, ok := spans[0].Payload.(*trace.Middleware)
	if !ok || p.Attempt != 2 || p.Outcome != outcomeOK || p.Wait < 30*time.Millisecond {
		t.Fatalf("unexpected retry payload %+v", spans[0].Payload)
	}
}

func TestRetryAfter_CappedByMaxBackoff(t *testing.T) {
	r := &retryLLM{cfg: &retryConfig{maxBackoff: 10 * time.Millisecond}}
	err := &llm.ProviderError{RetryAfter: time.Minute}

	if got := r.sleepFor(0, err); got != 10*time.Millisecond {
		t.Fatalf("sleep = %v, want 10ms", got)
	}
}

func TestGuardrails_BlockedEventAndSentinel(t *testing.T) {
	mw := NewGuardrails(WithMessageGuard("pii", blockMessageGuard))
	rec, ctx, call := recordedCall(t)

	_, err := llm.Use(okLLM("x"), mw).Execute(ctx, nil)
	if !errors.Is(err, llm.ErrGuardrailBlocked) {
		t.Fatalf("expected ErrGuardrailBlocked, got %v", err)
	}

	call.End(err)

	events := rec.Spans()[0].Events
	if len(events) != 1 || events[0].Name != eventGuardrailBlocked {
		t.Fatalf("expected a guardrail.blocked event, got %+v", events)
	}
}

func TestLimiter_WaitRecordsSpan(t *testing.T) {
	mw, stop, err := NewLimiter(50, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	rec, ctx, call := recordedCall(t)

	// The bucket starts empty, so the first call waits for a token.
	if _, err = llm.Use(okLLM("x"), mw).Execute(ctx, nil); err != nil {
		t.Fatal(err)
	}

	call.End(nil)

	spans := rec.ByKind(trace.KindMiddleware)
	if len(spans) != 1 {
		t.Fatalf("expected one limiter span, got %d", len(spans))
	}

	p, ok := spans[0].Payload.(*trace.Middleware)
	if !ok || p.Name != middlewareLimiter || p.Wait <= 0 || spans[0].Parent.SpanID != call.Context().SpanID {
		t.Fatalf("unexpected limiter span %+v", spans[0])
	}
}
