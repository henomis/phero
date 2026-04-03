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
	"sync"
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

// TestNewLimiter_Validation checks that invalid arguments are rejected.
func TestNewLimiter_Validation(t *testing.T) {
	tests := []struct {
		name    string
		rps     float64
		maxConc int
		wantErr error
	}{
		{"zero rate", 0, 1, ErrInvalidRate},
		{"negative rate", -1, 1, ErrInvalidRate},
		{"zero concurrency", 1, 0, ErrInvalidMaxConcurrentRequests},
		{"negative concurrency", 1, -1, ErrInvalidMaxConcurrentRequests},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, stop, err := NewLimiter(tc.rps, tc.maxConc)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			if stop != nil {
				t.Fatal("stop should be nil on error")
			}
		})
	}
}

// TestNewLimiter_ForwardsResult checks that a successful call is forwarded.
func TestNewLimiter_ForwardsResult(t *testing.T) {
	mw, stop, err := NewLimiter(100, 10)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
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

// TestNewLimiter_ForwardsError checks that inner errors are propagated.
func TestNewLimiter_ForwardsError(t *testing.T) {
	sentinel := errors.New("inner error")
	mw, stop, err := NewLimiter(100, 10)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	defer stop()

	client := llm.Use(errLLM(sentinel), mw)
	_, err = client.Execute(context.Background(), nil, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want %v", err, sentinel)
	}
}

// TestNewLimiter_MaxConcurrency verifies that at most maxConcurrentRequests
// calls run simultaneously.
func TestNewLimiter_MaxConcurrency(t *testing.T) {
	const maxConc = 3

	var (
		mu      sync.Mutex
		current int
		peak    int
	)

	gate := make(chan struct{})
	blocking := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		mu.Lock()
		current++
		if current > peak {
			peak = current
		}
		mu.Unlock()

		<-gate

		mu.Lock()
		current--
		mu.Unlock()

		return &llm.Result{Message: &llm.Message{}}, nil
	}}

	mw, stop, err := NewLimiter(1000, maxConc)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	defer stop()

	client := llm.Use(blocking, mw)

	const totalRequests = 9
	var wg sync.WaitGroup
	for range totalRequests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.Execute(context.Background(), nil, nil)
		}()
	}

	// Allow all goroutines to start and block, then release them in batches.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	mu.Lock()
	got := peak
	mu.Unlock()

	if got > maxConc {
		t.Fatalf("peak concurrency %d exceeded maxConcurrentRequests %d", got, maxConc)
	}
}

// TestNewLimiter_RateLimit verifies that the rate is enforced by measuring
// elapsed time when the bucket is empty.
func TestNewLimiter_RateLimit(t *testing.T) {
	const rps = 10.0

	mw, stop, err := NewLimiter(rps, 100)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	defer stop()

	client := llm.Use(okLLM("ok"), mw)

	// At high RPS the bucket fills almost instantly; just measure that
	// the next call after a fresh limiter doesn't take longer than 2 intervals.
	start := time.Now()
	_, err = client.Execute(context.Background(), nil, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	minExpected := time.Duration(float64(time.Second)/rps) / 2
	if elapsed < minExpected {
		t.Fatalf("elapsed %v is less than half the expected interval %v — rate limit may not be enforced", elapsed, minExpected)
	}
}

// TestNewLimiter_StopUnblocksTokenWaiter verifies that calling stop releases
// a caller blocked waiting for a rate-limit token.
func TestNewLimiter_StopUnblocksTokenWaiter(t *testing.T) {
	// Very low RPS so the bucket is effectively always empty.
	mw, stop, err := NewLimiter(0.001, 1)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}

	client := llm.Use(okLLM("ok"), mw)

	errCh := make(chan error, 1)
	go func() {
		_, err := client.Execute(context.Background(), nil, nil)
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	stop()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("got %v, want ErrStopped", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("caller was not unblocked after stop()")
	}
}

// TestNewLimiter_StopUnblocksSemaphoreWaiter verifies that calling stop
// releases a caller blocked waiting for a concurrency slot.
func TestNewLimiter_StopUnblocksSemaphoreWaiter(t *testing.T) {
	// High RPS so tokens are never the bottleneck; concurrency capped at 1.
	mw, stop, err := NewLimiter(1000, 1)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}

	gate := make(chan struct{})
	blocking := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		<-gate
		return &llm.Result{Message: &llm.Message{}}, nil
	}}

	client := llm.Use(blocking, mw)

	// First call occupies the single concurrency slot.
	go func() { _, _ = client.Execute(context.Background(), nil, nil) }()
	time.Sleep(20 * time.Millisecond)

	// Second call blocks on the semaphore.
	errCh := make(chan error, 1)
	go func() {
		_, err := client.Execute(context.Background(), nil, nil)
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	stop()
	close(gate) // release the first caller too

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("got %v, want ErrStopped", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("semaphore waiter was not unblocked after stop()")
	}
}

// TestNewLimiter_StopIdempotent verifies that calling stop more than once
// does not panic.
func TestNewLimiter_StopIdempotent(t *testing.T) {
	_, stop, err := NewLimiter(10, 1)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	stop()
	stop() // must not panic
}

// TestNewLimiter_ContextCancelToken verifies that context cancellation
// unblocks a caller waiting for a rate-limit token.
func TestNewLimiter_ContextCancelToken(t *testing.T) {
	mw, stop, err := NewLimiter(0.001, 1)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	defer stop()

	ctx, cancel := context.WithCancel(context.Background())
	client := llm.Use(okLLM("ok"), mw)

	errCh := make(chan error, 1)
	go func() {
		_, err := client.Execute(ctx, nil, nil)
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("caller was not unblocked after context cancellation")
	}
}

// TestNewLimiter_ContextCancelSemaphore verifies that context cancellation
// unblocks a caller waiting for a concurrency slot.
func TestNewLimiter_ContextCancelSemaphore(t *testing.T) {
	mw, stop, err := NewLimiter(1000, 1)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	defer stop()

	gate := make(chan struct{})
	blocking := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		<-gate
		return &llm.Result{Message: &llm.Message{}}, nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	client := llm.Use(blocking, mw)

	go func() { _, _ = client.Execute(context.Background(), nil, nil) }()
	time.Sleep(20 * time.Millisecond)

	errCh := make(chan error, 1)
	go func() {
		_, err := client.Execute(ctx, nil, nil)
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	close(gate)

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("caller was not unblocked after context cancellation")
	}
}

// TestNewLimiter_MultiAgentSharedConcurrency verifies that the concurrency cap
// is enforced globally when the same middleware is applied to multiple agent
// LLM instances.
func TestNewLimiter_MultiAgentSharedConcurrency(t *testing.T) {
	const maxConc = 2

	var (
		mu      sync.Mutex
		current int
		peak    int
	)

	gate := make(chan struct{})
	blocking := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		mu.Lock()
		current++
		if current > peak {
			peak = current
		}
		mu.Unlock()

		<-gate

		mu.Lock()
		current--
		mu.Unlock()

		return &llm.Result{Message: &llm.Message{}}, nil
	}}

	// One limiter, applied to three independent "agents".
	mw, stop, err := NewLimiter(1000, maxConc)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	defer stop()

	agent1 := llm.Use(blocking, mw)
	agent2 := llm.Use(blocking, mw)
	agent3 := llm.Use(blocking, mw)

	const callsPerAgent = 3
	var wg sync.WaitGroup
	for _, agent := range []llm.LLM{agent1, agent2, agent3} {
		for range callsPerAgent {
			wg.Add(1)
			go func(a llm.LLM) {
				defer wg.Done()
				_, _ = a.Execute(context.Background(), nil, nil)
			}(agent)
		}
	}

	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()

	mu.Lock()
	got := peak
	mu.Unlock()

	if got > maxConc {
		t.Fatalf("peak concurrency %d exceeded global maxConcurrentRequests %d across agents", got, maxConc)
	}
}

// TestNewLimiter_MultiAgentSharedRate verifies that a depleted token bucket
// blocks callers from all agents, not just from one.
func TestNewLimiter_MultiAgentSharedRate(t *testing.T) {
	// Extremely low rate: one token every ~1000 seconds — bucket stays empty.
	mw, stop, err := NewLimiter(0.001, 100)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}
	defer stop()

	agent1 := llm.Use(okLLM("a1"), mw)
	agent2 := llm.Use(okLLM("a2"), mw)

	results := make(chan error, 2)
	for _, a := range []llm.LLM{agent1, agent2} {
		go func(a llm.LLM) {
			_, err := a.Execute(context.Background(), nil, nil)
			results <- err
		}(a)
	}

	// Neither agent should complete before stop() drains the wait.
	time.Sleep(30 * time.Millisecond)
	select {
	case <-results:
		t.Fatal("an agent completed before the rate limit was released")
	default:
	}

	stop()

	// Both blocked callers should now receive ErrStopped.
	for range 2 {
		select {
		case err := <-results:
			if !errors.Is(err, ErrStopped) {
				t.Fatalf("got %v, want ErrStopped", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("caller not unblocked after stop()")
		}
	}
}
