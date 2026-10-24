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
)

// TestNewRetry_Validation checks that zero/negative maxAttempts is rejected.
func TestNewRetry_Validation(t *testing.T) {
	for _, n := range []int{0, -1} {
		_, err := NewRetry(n)
		if !errors.Is(err, ErrInvalidMaxAttempts) {
			t.Errorf("maxAttempts=%d: got %v, want ErrInvalidMaxAttempts", n, err)
		}
	}
}

// TestNewRetry_SuccessOnFirstAttempt checks that a successful call is returned immediately.
func TestNewRetry_SuccessOnFirstAttempt(t *testing.T) {
	mw, err := NewRetry(3, WithInitialBackoff(0))
	if err != nil {
		t.Fatalf("NewRetry: %v", err)
	}

	client := llm.Use(okLLM("ok"), mw)

	result, err := client.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Message.TextContent() != "ok" {
		t.Fatalf("got %q, want %q", result.Message.TextContent(), "ok")
	}
}

// TestNewRetry_RetriesAndSucceeds verifies that a transient error is retried
// and the eventual success is returned.
func TestNewRetry_RetriesAndSucceeds(t *testing.T) {
	callCount := 0
	transient := &llm.ProviderError{Provider: "p", StatusCode: 503, Err: errors.New("transient")}

	inner := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []llm.CallOption) (*llm.Result, error) {
		callCount++
		if callCount < 3 {
			return nil, transient
		}

		return &llm.Result{Message: &llm.Message{Parts: []llm.ContentPart{llm.Text("success")}}}, nil
	}}

	mw, err := NewRetry(5, WithInitialBackoff(0))
	if err != nil {
		t.Fatalf("NewRetry: %v", err)
	}

	client := llm.Use(inner, mw)

	result, execErr := client.Execute(context.Background(), nil)
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}

	if callCount != 3 {
		t.Fatalf("callCount=%d, want 3", callCount)
	}

	if result.Message.TextContent() != "success" {
		t.Fatalf("got %q, want %q", result.Message.TextContent(), "success")
	}
}

// TestNewRetry_ExhaustsAttempts checks that MaxAttemptsExceededError is returned
// when all retries fail.
func TestNewRetry_ExhaustsAttempts(t *testing.T) {
	sentinel := errors.New("always fails")

	mw, err := NewRetry(3, WithInitialBackoff(0))
	if err != nil {
		t.Fatalf("NewRetry: %v", err)
	}

	client := llm.Use(errLLM(&llm.ProviderError{Provider: "p", StatusCode: 500, Err: sentinel}), mw)
	_, execErr := client.Execute(context.Background(), nil)

	var maxErr *MaxAttemptsExceededError
	if !errors.As(execErr, &maxErr) {
		t.Fatalf("got %T %v, want MaxAttemptsExceededError", execErr, execErr)
	}

	if maxErr.Attempts != 3 {
		t.Fatalf("Attempts=%d, want 3", maxErr.Attempts)
	}

	if !errors.Is(execErr, sentinel) {
		t.Fatalf("Unwrap should yield sentinel error")
	}
}

// TestNewRetry_ShouldRetryFalse verifies that when the predicate returns false
// the error is returned immediately without further attempts.
func TestNewRetry_ShouldRetryFalse(t *testing.T) {
	callCount := 0
	fatal := errors.New("fatal")

	inner := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []llm.CallOption) (*llm.Result, error) {
		callCount++
		return nil, fatal
	}}

	mw, err := NewRetry(5,
		WithInitialBackoff(0),
		WithShouldRetry(func(err error) bool { return !errors.Is(err, fatal) }),
	)
	if err != nil {
		t.Fatalf("NewRetry: %v", err)
	}

	client := llm.Use(inner, mw)

	_, execErr := client.Execute(context.Background(), nil)
	if !errors.Is(execErr, fatal) {
		t.Fatalf("got %v, want fatal error", execErr)
	}

	if callCount != 1 {
		t.Fatalf("callCount=%d, want 1 (no retry)", callCount)
	}
}

// TestNewRetry_ContextCancellation verifies that a cancelled context is
// honoured between retry attempts.
func TestNewRetry_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	inner := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []llm.CallOption) (*llm.Result, error) {
		callCount++

		cancel() // cancel after first attempt

		return nil, &llm.ProviderError{Provider: "p", StatusCode: 503, Err: errors.New("transient")}
	}}

	mw, err := NewRetry(5, WithInitialBackoff(10*time.Millisecond))
	if err != nil {
		t.Fatalf("NewRetry: %v", err)
	}

	client := llm.Use(inner, mw)

	_, execErr := client.Execute(ctx, nil)
	if !errors.Is(execErr, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", execErr)
	}

	if callCount != 1 {
		t.Fatalf("callCount=%d, want 1", callCount)
	}
}

// TestNewRetry_DefaultSkipsPermanentErrors verifies that by default errors a
// retry cannot fix are returned after one attempt, and retryable ones are not.
func TestNewRetry_DefaultSkipsPermanentErrors(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		calls int
	}{
		{"bad request", &llm.ProviderError{Provider: "p", StatusCode: 400, Err: errors.New("bad")}, 1},
		{"unauthorized", &llm.ProviderError{Provider: "p", StatusCode: 401, Err: errors.New("key")}, 1},
		{"context length", &llm.ProviderError{Provider: "p", StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("long")}, 1},
		{"unknown", errors.New("local validation"), 1},
		{"rate limit", &llm.ProviderError{Provider: "p", StatusCode: 429, Err: errors.New("slow down")}, 3},
		{"unavailable", &llm.ProviderError{Provider: "p", StatusCode: 503, Err: errors.New("down")}, 3},
	}

	for _, tc := range cases {
		calls := 0
		inner := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []llm.CallOption) (*llm.Result, error) {
			calls++
			return nil, tc.err
		}}

		mw, err := NewRetry(3, WithInitialBackoff(0))
		if err != nil {
			t.Fatalf("NewRetry: %v", err)
		}

		_, execErr := llm.Use(inner, mw).Execute(context.Background(), nil)
		if !errors.Is(execErr, tc.err) {
			t.Errorf("%s: got %v, want the provider error", tc.name, execErr)
		}

		if calls != tc.calls {
			t.Errorf("%s: calls = %d, want %d", tc.name, calls, tc.calls)
		}
	}
}
