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
	"sync"
	"time"

	"github.com/henomis/phero/llm"
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
func (l *limiterLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	select {
	case <-l.tokens:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.stopCh:
		return nil, ErrStopped
	}

	select {
	case l.semaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.stopCh:
		return nil, ErrStopped
	}
	defer func() { <-l.semaphore }()

	return l.inner.Execute(ctx, messages, tools)
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

// NewLimiter returns an llm.LLMMiddleware that enforces two constraints on
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
func NewLimiter(requestsPerSecond float64, maxConcurrentRequests int) (llm.LLMMiddleware, func(), error) {
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
