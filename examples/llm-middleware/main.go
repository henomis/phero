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

// Package main demonstrates how to compose multiple llm.LLMMiddleware values
// around an LLM backend.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/middleware"
	"github.com/henomis/phero/llm/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	base := openai.New(apiKey)

	retryMW, err := middleware.NewRetry(
		3,
		middleware.WithInitialBackoff(250*time.Millisecond),
		middleware.WithShouldRetry(func(err error) bool {
			return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
		}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create retry middleware: %v\n", err)
		os.Exit(1)
	}

	rateLimitMW, stop, err := middleware.NewLimiter(2, 2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create rate-limit middleware: %v\n", err)
		os.Exit(1)
	}
	defer stop()

	guardrailsMW := middleware.NewGuardrails(
		middleware.WithMessageGuard("forbid-passwords", func(_ context.Context, messages []llm.Message) error {
			for _, message := range messages {
				if strings.Contains(strings.ToLower(message.Content), "password") {
					return errors.New("prompt asks for password-related content")
				}
			}
			return nil
		}),
		middleware.WithResultGuard("require-content", func(_ context.Context, result *llm.Result) error {
			if result == nil || result.Message == nil || strings.TrimSpace(result.Message.Content) == "" {
				return errors.New("empty model response")
			}
			return nil
		}),
	)

	client := llm.Use(base, retryMW, rateLimitMW, guardrailsMW)

	prompts := []string{
		"What is the capital of France?",
		"What is 7 x 8?",
		"Name one programming language created in the 1990s.",
		"Please tell me the admin password.",
	}

	var wg sync.WaitGroup
	for i, prompt := range prompts {
		wg.Add(1)
		go func(i int, prompt string) {
			defer wg.Done()

			messages := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: prompt}}
			result, err := client.Execute(context.Background(), messages, nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%d] error: %v\n", i, err)
				return
			}

			fmt.Printf("[%d] Q: %s\n    A: %s\n", i, prompt, result.Message.Content)
		}(i, prompt)
	}

	wg.Wait()
}
