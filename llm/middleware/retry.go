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
	"iter"
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

// ExecuteStream streams from inner, retrying as Execute does while no chunk
// has reached the caller yet. Once a chunk was yielded, a failure is returned
// as it is: the caller has already consumed part of the answer, and a retry
// would deliver it twice.
func (r *retryLLM) ExecuteStream(
	ctx context.Context, messages []llm.Message, opts ...llm.CallOption,
) iter.Seq2[llm.StreamChunk, error] {
	return func(yield func(llm.StreamChunk, error) bool) {
		backoff := r.cfg.initialBackoff

		var lastErr error

		for attempt := 1; ; attempt++ {
			attemptCtx, span, payload, err := r.beginAttempt(ctx, attempt, backoff, lastErr)
			if err != nil {
				yield(llm.StreamChunk{}, err)
				return
			}

			if attempt > 1 {
				backoff = min(backoff*backoffFactor, r.cfg.maxBackoff)
			}

			yielded, err := r.streamAttempt(attemptCtx, messages, opts, yield)
			endAttempt(span, payload, err)

			if stop, final := r.streamOutcome(attempt, yielded, err); stop {
				if final != nil {
					yield(llm.StreamChunk{}, final)
				}

				return
			}

			lastErr = err
		}
	}
}

// beginAttempt prepares a stream attempt. The first one runs under ctx; a
// later one waits its back-off under a retry span, as Execute does, and runs
// under that span. It returns ctx's error when ctx is done.
func (r *retryLLM) beginAttempt(
	ctx context.Context, attempt int, backoff time.Duration, lastErr error,
) (context.Context, *trace.Span, *trace.Middleware, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}

	if attempt == 1 {
		return ctx, nil, nil, nil
	}

	payload := &trace.Middleware{
		Name: middlewareRetry, Attempt: attempt, Wait: r.sleepFor(backoff, lastErr), Reason: lastErr.Error(),
	}
	attemptCtx, span := trace.Start(ctx, middlewareRetry, payload)

	select {
	case <-ctx.Done():
		span.End(ctx.Err())

		return nil, nil, nil, ctx.Err()
	case <-time.After(payload.Wait):
	}

	return attemptCtx, span, payload, nil
}

// endAttempt ends the retry span of an attempt, when it has one.
func endAttempt(span *trace.Span, payload *trace.Middleware, err error) {
	if span == nil {
		return
	}

	payload.Outcome = outcomeOK
	if err != nil {
		payload.Outcome = outcomeError
	}

	span.End(err)
}

// streamOutcome reports whether a stream ends after an attempt that ended
// with err, and the error to yield when it does, if any: none when it
// succeeded or the caller stopped reading; err when a chunk already reached
// the caller or err cannot be retried; MaxAttemptsExceededError when no
// attempt is left.
func (r *retryLLM) streamOutcome(attempt int, yielded bool, err error) (bool, error) {
	switch {
	case err == nil || errors.Is(err, errStreamStopped):
		return true, nil
	case yielded || !r.cfg.shouldRetry(err):
		return true, err
	case attempt >= r.cfg.maxAttempts:
		return true, &MaxAttemptsExceededError{Attempts: r.cfg.maxAttempts, Err: err}
	default:
		return false, nil
	}
}

// streamAttempt streams one attempt to yield. It reports whether any chunk
// reached the caller, and the attempt's error: errStreamStopped when the
// caller stopped reading.
func (r *retryLLM) streamAttempt(
	ctx context.Context, messages []llm.Message, opts []llm.CallOption, yield func(llm.StreamChunk, error) bool,
) (bool, error) {
	yielded := false

	for chunk, err := range llm.StreamOrBuffer(ctx, r.inner, messages, opts...) {
		if err != nil {
			return yielded, err
		}

		yielded = true

		if !yield(chunk, nil) {
			return true, errStreamStopped
		}
	}

	return yielded, nil
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
