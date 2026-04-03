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

	"github.com/henomis/phero/llm"
)

// stubLLM is a minimal llm.LLM for testing.
type stubLLM struct {
	fn func(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error)
}

func (s *stubLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	return s.fn(ctx, messages, tools)
}

func okLLM(content string) *stubLLM {
	return &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return &llm.Result{Message: &llm.Message{Content: content}}, nil
	}}
}

func errLLM(err error) *stubLLM {
	return &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return nil, err
	}}
}

// TestNewRateLimit_Validation checks that invalid arguments are rejected.
func TestNewRateLimit_Validation(t *testing.T) {
	tests := []struct {
		name    string
		rps     float64
		buf     int
		wantErr error
	}{
		{"zero rate", 0, 0, ErrInvalidRate},
		{"negative rate", -1, 0, ErrInvalidRate},
		{"negative buffer", 1, -1, ErrInvalidBufferSize},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, stop, err := NewRateLimit(tc.rps, tc.buf)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			if stop != nil {
				t.Fatal("stop should be nil on error")
			}
		})
	}
}

// TestNewRateLimit_ForwardsResult checks that a successful call is forwarded.
func TestNewRateLimit_ForwardsResult(t *testing.T) {
	mw, stop, err := NewRateLimit(100, 1)
	if err != nil {
		t.Fatalf("NewRateLimit: %v", err)
	}
	defer stop()

	client := llm.Use(okLLM("hello"), mw)
	result, err := client.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Message.Content != "hello" {
		t.Fatalf("got %q, want %q", result.Message.Content, "hello")
	}
}

// TestNewRateLimit_ForwardsError checks that inner errors are propagated.
func TestNewRateLimit_ForwardsError(t *testing.T) {
	sentinel := errors.New("inner error")
	mw, stop, err := NewRateLimit(100, 1)
	if err != nil {
		t.Fatalf("NewRateLimit: %v", err)
	}
	defer stop()

	client := llm.Use(errLLM(sentinel), mw)
	_, err = client.Execute(context.Background(), nil, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want %v", err, sentinel)
	}
}

// TestNewRateLimit_ContextCancellation checks that a cancelled context is
// respected while waiting in the queue.
func TestNewRateLimit_ContextCancellation(t *testing.T) {
	// rate = 0.01 RPS => 100s interval; buffer = 0 so the second caller blocks
	mw, stop, err := NewRateLimit(0.01, 0)
	if err != nil {
		t.Fatalf("NewRateLimit: %v", err)
	}
	defer stop()

	slow := &stubLLM{fn: func(ctx context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}

	client := llm.Use(slow, mw)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = client.Execute(ctx, nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
}

// TestNewRateLimit_StopDrainsQueue checks that calling stop unblocks waiters.
func TestNewRateLimit_StopDrainsQueue(t *testing.T) {
	// rate = 0.01 RPS => very slow dispatch
	mw, stop, err := NewRateLimit(0.01, 1)
	if err != nil {
		t.Fatalf("NewRateLimit: %v", err)
	}

	client := llm.Use(okLLM("x"), mw)

	// Start a call that will sit in the queue.
	done := make(chan error, 1)
	go func() {
		_, err := client.Execute(context.Background(), nil, nil)
		done <- err
	}()

	// Give the goroutine time to enter the queue.
	time.Sleep(20 * time.Millisecond)
	stop()

	select {
	case err := <-done:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("got %v, want ErrStopped", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for stop to drain queue")
	}
}

// TestNewRateLimit_StopIdempotent checks that stop can be called multiple times.
func TestNewRateLimit_StopIdempotent(t *testing.T) {
	_, stop, err := NewRateLimit(1, 0)
	if err != nil {
		t.Fatalf("NewRateLimit: %v", err)
	}
	stop()
	stop() // must not panic
}
