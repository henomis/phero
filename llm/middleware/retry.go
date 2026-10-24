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
	"math/rand"
	"time"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

const (
	defaultInitialBackoff = 500 * time.Millisecond
	defaultMaxBackoff     = 30 * time.Second
	jitterHalfDivisor     = 2
	jitterQuarterDivisor  = 4
	backoffFactor         = 2
)

// RetryOption configures a Retry middleware.
type RetryOption func(*retryConfig)

// retryConfig holds the configuration for the Retry middleware.
type retryConfig struct {
	maxAttempts    int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	shouldRetry    func(error) bool
}

// WithInitialBackoff sets the initial back-off duration before the first retry.
// It doubles on each subsequent attempt (capped by WithMaxBackoff). Default: 500ms.
func WithInitialBackoff(d time.Duration) RetryOption {
	return func(c *retryConfig) { c.initialBackoff = d }
}

// WithMaxBackoff caps the exponential back-off duration. Default: 30s.
func WithMaxBackoff(d time.Duration) RetryOption {
	return func(c *retryConfig) { c.maxBackoff = d }
}

// WithShouldRetry replaces the default retry predicate. By default only
// errors a retry can fix are retried, as reported by llm.IsRetryable: rate
// limits, timeouts, server and overload errors and network failures. A bad
// request, a bad API key or a prompt too long for the context window is
// returned at once.
func WithShouldRetry(fn func(error) bool) RetryOption {
	return func(c *retryConfig) { c.shouldRetry = fn }
}

// NewRetry returns an llm.Middleware that automatically retries failed
// Execute calls up to maxAttempts times using exponential back-off with jitter.
// Only errors that llm.IsRetryable reports as retryable are retried, unless
// WithShouldRetry says otherwise.
//
// Context cancellation is honoured between retries: if ctx is cancelled while
// sleeping before a retry, the middleware returns immediately.
//
//	mw, err := middleware.NewRetry(3, middleware.WithInitialBackoff(200*time.Millisecond))
//	if err != nil { ... }
//	client := llm.Use(base, mw)
func NewRetry(maxAttempts int, opts ...RetryOption) (llm.Middleware, error) {
	if maxAttempts < 1 {
		return nil, ErrInvalidMaxAttempts
	}

	cfg := &retryConfig{
		maxAttempts:    maxAttempts,
		initialBackoff: defaultInitialBackoff,
		maxBackoff:     defaultMaxBackoff,
		shouldRetry:    llm.IsRetryable,
	}
	for _, o := range opts {
		o(cfg)
	}

	return func(next llm.LLM) llm.LLM {
		return &retryLLM{inner: next, cfg: cfg}
	}, nil
}

// retryLLM is the concrete LLM produced by the Retry middleware.
type retryLLM struct {
	inner llm.LLM
	cfg   *retryConfig
}

// Execute calls inner.Execute up to maxAttempts times, sleeping with
// exponential back-off and +/-25% jitter between each attempt. When the error
// is an *llm.ProviderError with a RetryAfter hint, the sleep is at least that
// long, still capped by the maximum back-off.
//
// Every attempt after the first runs under a trace span with a
// *trace.Middleware payload named "retry", covering the wait and the call.
func (r *retryLLM) Execute(ctx context.Context, messages []llm.Message, opts ...llm.CallOption) (*llm.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	result, err := r.inner.Execute(ctx, messages, opts...)
	if err == nil {
		return result, nil
	}

	backoff := r.cfg.initialBackoff

	for attempt := 2; attempt <= r.cfg.maxAttempts; attempt++ {
		if !r.cfg.shouldRetry(err) {
			return nil, err
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		payload := &trace.Middleware{
			Name: middlewareRetry, Attempt: attempt, Wait: r.sleepFor(backoff, err), Reason: err.Error(),
		}
		attemptCtx, span := trace.Start(ctx, middlewareRetry, payload)

		select {
		case <-ctx.Done():
			span.End(ctx.Err())
			return nil, ctx.Err()
		case <-time.After(payload.Wait):
		}

		result, err = r.inner.Execute(attemptCtx, messages, opts...)
		if err == nil {
			payload.Outcome = outcomeOK

			span.End(nil)

			return result, nil
		}

		payload.Outcome = outcomeError

		span.End(err)

		backoff = min(backoff*backoffFactor, r.cfg.maxBackoff)
	}

	if !r.cfg.shouldRetry(err) {
		return nil, err
	}

	return nil, &MaxAttemptsExceededError{Attempts: r.cfg.maxAttempts, Err: err}
}

// sleepFor returns the jittered back-off, raised to the provider's RetryAfter
// hint when err carries one, and capped by the maximum back-off.
func (r *retryLLM) sleepFor(backoff time.Duration, err error) time.Duration {
	sleep := backoff
	if half := int64(backoff) / jitterHalfDivisor; half > 0 {
		jitter := time.Duration(rand.Int63n(half)) - backoff/jitterQuarterDivisor //nolint:gosec

		sleep = max(sleep+jitter, 0)
	}

	var perr *llm.ProviderError
	if errors.As(err, &perr) && perr.RetryAfter > sleep {
		sleep = perr.RetryAfter
	}

	return min(sleep, r.cfg.maxBackoff)
}
