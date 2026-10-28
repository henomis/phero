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
	"iter"
	"sync"
	"time"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

// limiterLLM enforces both a per-second rate limit and a maximum concurrency
// limit on Execute calls.
type limiterLLM struct {
	inner     llm.LLM
	tokens    chan struct{}
	semaphore chan struct{}
	stopCh    chan struct{}
}

// Execute acquires a rate-limit token and a concurrency slot before forwarding
// the call to the inner LLM. Both acquisitions respect ctx cancellation and
// middleware shutdown.
//
// When a call has to wait, the wait is recorded as a trace span with a
// *trace.Middleware payload named "limiter".
func (l *limiterLLM) Execute(ctx context.Context, messages []llm.Message, opts ...llm.CallOption) (*llm.Result, error) {
	if err := l.acquire(ctx); err != nil {
		return nil, err
	}

	defer func() { <-l.semaphore }()

	return l.inner.Execute(ctx, messages, opts...)
}

// ExecuteStream acquires a rate-limit token and a concurrency slot, as Execute
// does, and holds the slot until the stream ends or the caller stops reading.
func (l *limiterLLM) ExecuteStream(
	ctx context.Context, messages []llm.Message, opts ...llm.CallOption,
) iter.Seq2[llm.StreamChunk, error] {
	return func(yield func(llm.StreamChunk, error) bool) {
		if err := l.acquire(ctx); err != nil {
			yield(llm.StreamChunk{}, err)
			return
		}

		defer func() { <-l.semaphore }()

		for chunk, err := range llm.StreamOrBuffer(ctx, l.inner, messages, opts...) {
			if !yield(chunk, err) || err != nil {
				return
			}
		}
	}
}

// acquire takes a rate-limit token and a concurrency slot. If either is not
// immediately available it records the wait as a span.
func (l *limiterLLM) acquire(ctx context.Context) error {
	select {
	case <-l.tokens:
		select {
		case l.semaphore <- struct{}{}:
			return nil
		default:
		}

		return l.wait(ctx, false)
	default:
	}

	return l.wait(ctx, true)
}

// wait blocks for a token (when needToken) and then a slot, inside a span.
func (l *limiterLLM) wait(ctx context.Context, needToken bool) error {
	payload := &trace.Middleware{Name: middlewareLimiter}
	start := time.Now()
	_, span := trace.Start(ctx, middlewareLimiter, payload, trace.WithStartTime(start))

	err := l.block(ctx, needToken)
	payload.Wait = time.Since(start)

	payload.Outcome = outcomeOK
	if err != nil {
		payload.Outcome = outcomeError
	}

	span.End(err)

	return err
}

func (l *limiterLLM) block(ctx context.Context, needToken bool) error {
	if needToken {
		select {
		case <-l.tokens:
		case <-ctx.Done():
			return ctx.Err()
		case <-l.stopCh:
			return ErrStopped
		}
	}

	select {
	case l.semaphore <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-l.stopCh:
		return ErrStopped
	}
}

// runLimiterTokenProducer adds one token to the bucket on each ticker
// interval, discarding tokens when the bucket is already full.
func runLimiterTokenProducer(tokens chan struct{}, interval time.Duration, stopCh chan struct{}) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			select {
			case tokens <- struct{}{}:
			default:
				// bucket full; discard token
			}
		}
	}
}

// NewLimiter returns an llm.Middleware that enforces two constraints on
// Execute calls:
//
//   - At most requestsPerSecond calls are started per second (token bucket).
//   - At most maxConcurrentRequests calls execute concurrently (semaphore).
//
// The bucket is pre-filled up to one second worth of tokens so the first burst
// of requests is not artificially delayed.
//
// The returned stop function shuts down the background goroutine and is safe
// to call more than once.
//
//	mw, stop, err := middleware.NewLimiter(5.0, 3)
//	if err != nil { ... }
//	defer stop()
//	client := llm.Use(openaiClient, mw)
func NewLimiter(requestsPerSecond float64, maxConcurrentRequests int) (llm.Middleware, func(), error) {
	if requestsPerSecond <= 0 {
		return nil, nil, ErrInvalidRate
	}

	if maxConcurrentRequests <= 0 {
		return nil, nil, ErrInvalidMaxConcurrentRequests
	}

	interval := time.Duration(float64(time.Second) / requestsPerSecond)
	bucketCapacity := max(1, int(requestsPerSecond))

	stopCh := make(chan struct{})

	var stopOnce sync.Once

	stop := func() {
		stopOnce.Do(func() { close(stopCh) })
	}

	// tokens and semaphore are shared across all applications of this
	// middleware so that the rate and concurrency limits are global even
	// when multiple agents each wrap their own LLM with the same limiter.
	tokens := make(chan struct{}, bucketCapacity)
	semaphore := make(chan struct{}, maxConcurrentRequests)

	go runLimiterTokenProducer(tokens, interval, stopCh)

	mw := func(next llm.LLM) llm.LLM {
		return &limiterLLM{
			inner:     next,
			tokens:    tokens,
			semaphore: semaphore,
			stopCh:    stopCh,
		}
	}

	return mw, stop, nil
}
