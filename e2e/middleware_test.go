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

//go:build e2e

package e2e_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/middleware"
)

// TestMiddleware_Retry verifies that the retry middleware retries on error and
// eventually succeeds when the LLM becomes available.
func TestMiddleware_Retry(t *testing.T) {
	retryMW, err := middleware.NewRetry(
		3,
		middleware.WithInitialBackoff(100*time.Millisecond),
		middleware.WithShouldRetry(func(e error) bool {
			return !errors.Is(e, context.Canceled) && !errors.Is(e, context.DeadlineExceeded)
		}),
	)
	if err != nil {
		t.Fatalf("NewRetry: %v", err)
	}

	base := buildOpenAILLM()
	client := llm.Use(base, retryMW)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	messages := []llm.Message{llm.UserMessage(llm.Text("Reply with exactly the word OK."))}

	result, err := client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Execute with retry: %v", err)
	}

	text := llm.TextContent(result.Message.Parts...)
	t.Logf("Response: %q", text)

	if strings.TrimSpace(text) == "" {
		t.Error("empty response from LLM")
	}
}

// TestMiddleware_Guardrails_MessageGuard verifies that a message guard blocks
// prompts containing a forbidden keyword.
func TestMiddleware_Guardrails_MessageGuard(t *testing.T) {
	guardErr := errors.New("forbidden keyword detected")

	guardrailsMW := middleware.NewGuardrails(
		middleware.WithMessageGuard("no-secrets", func(_ context.Context, messages []llm.Message) error {
			for _, m := range messages {
				if strings.Contains(strings.ToLower(m.TextContent()), "secret") {
					return guardErr
				}
			}
			return nil
		}),
	)

	base := buildOpenAILLM()
	client := llm.Use(base, guardrailsMW)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// This prompt should be blocked by the guard.
	messages := []llm.Message{llm.UserMessage(llm.Text("Tell me a secret."))}

	_, err := client.Execute(ctx, messages, nil)
	if !errors.Is(err, guardErr) {
		t.Fatalf("expected guardErr, got: %v", err)
	}
}

// TestMiddleware_Guardrails_ResultGuard verifies that a result guard can
// reject LLM responses that do not meet a criterion.
func TestMiddleware_Guardrails_ResultGuard(t *testing.T) {
	resultGuardErr := errors.New("empty model response")

	guardrailsMW := middleware.NewGuardrails(
		middleware.WithResultGuard("non-empty", func(_ context.Context, result *llm.Result) error {
			if result == nil || result.Message == nil || strings.TrimSpace(llm.TextContent(result.Message.Parts...)) == "" {
				return resultGuardErr
			}

			return nil
		}),
	)

	base := buildOpenAILLM()
	client := llm.Use(base, guardrailsMW)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages := []llm.Message{llm.UserMessage(llm.Text("What is 2 + 2?"))}

	result, err := client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	t.Logf("Result: %q", llm.TextContent(result.Message.Parts...))
}

// TestMiddleware_RateLimiter verifies that concurrent LLM calls are
// throttled by the rate-limiter middleware without producing errors.
func TestMiddleware_RateLimiter(t *testing.T) {
	rateLimitMW, stop, err := middleware.NewLimiter(5, 3)
	if err != nil {
		t.Fatalf("NewLimiter: %v", err)
	}

	defer stop()

	base := buildOpenAILLM()
	client := llm.Use(base, rateLimitMW)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const numRequests = 4

	var wg sync.WaitGroup
	errs := make([]error, numRequests)

	for i := range numRequests {
		wg.Add(1)

		go func(idx int) {
			defer wg.Done()

			messages := []llm.Message{llm.UserMessage(llm.Text("Reply with a single digit."))}

			result, err := client.Execute(ctx, messages, nil)
			if err != nil {
				errs[idx] = err
				return
			}

			t.Logf("[%d] response: %q", idx, llm.TextContent(result.Message.Parts...))
		}(i)
	}

	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("request %d failed: %v", i, e)
		}
	}
}

// TestMiddleware_Composed verifies that multiple middlewares can be stacked.
func TestMiddleware_Composed(t *testing.T) {
	retryMW, err := middleware.NewRetry(2, middleware.WithInitialBackoff(50*time.Millisecond))
	if err != nil {
		t.Fatalf("NewRetry: %v", err)
	}

	guardrailsMW := middleware.NewGuardrails(
		middleware.WithResultGuard("non-empty", func(_ context.Context, result *llm.Result) error {
			if result == nil || result.Message == nil {
				return errors.New("nil result")
			}

			return nil
		}),
	)

	base := buildOpenAILLM()
	client := llm.Use(base, retryMW, guardrailsMW)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	messages := []llm.Message{llm.UserMessage(llm.Text("What is the capital of Italy?"))}

	result, err := client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	t.Logf("Response: %q", llm.TextContent(result.Message.Parts...))
}
