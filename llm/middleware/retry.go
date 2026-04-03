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
	"math/rand"
	"time"

	"github.com/henomis/phero/llm"
)

const (
	defaultInitialBackoff = 500 * time.Millisecond
	defaultMaxBackoff     = 30 * time.Second
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

// WithShouldRetry replaces the default retry predicate. By default every
// non-nil error triggers a retry.
func WithShouldRetry(fn func(error) bool) RetryOption {
	return func(c *retryConfig) { c.shouldRetry = fn }
}

// NewRetry returns an llm.LLMMiddleware that automatically retries failed
// Execute calls up to maxAttempts times using exponential back-off with jitter.
//
// Context cancellation is honoured between retries: if ctx is cancelled while
// sleeping before a retry, the middleware returns immediately.
//
//	mw, err := middleware.NewRetry(3, middleware.WithInitialBackoff(200*time.Millisecond))
//	if err != nil { ... }
//	client := llm.Use(base, mw)
func NewRetry(maxAttempts int, opts ...RetryOption) (llm.LLMMiddleware, error) {
	if maxAttempts < 1 {
		return nil, ErrInvalidMaxAttempts
	}

	cfg := &retryConfig{
		maxAttempts:    maxAttempts,
		initialBackoff: defaultInitialBackoff,
		maxBackoff:     defaultMaxBackoff,
		shouldRetry:    func(error) bool { return true },
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
// exponential back-off and +/-25% jitter between each attempt.
func (r *retryLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	var lastErr error
	backoff := r.cfg.initialBackoff

	for attempt := 0; attempt < r.cfg.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		result, err := r.inner.Execute(ctx, messages, tools)
		if err == nil {
			return result, nil
		}
		lastErr = err

		if !r.cfg.shouldRetry(err) {
			return nil, err
		}

		if attempt == r.cfg.maxAttempts-1 {
			break
		}

		sleep := backoff
		if half := int64(backoff) / 2; half > 0 {
			jitter := time.Duration(rand.Int63n(half)) - backoff/4 //nolint:gosec
			sleep += jitter
			if sleep < 0 {
				sleep = 0
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(sleep):
		}

		backoff *= 2
		if backoff > r.cfg.maxBackoff {
			backoff = r.cfg.maxBackoff
		}
	}

	return nil, &MaxAttemptsExceededError{Attempts: r.cfg.maxAttempts, Err: lastErr}
}
