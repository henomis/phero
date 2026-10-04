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
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/v2/llm"
)

type cityFacts struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

// TestLLM_ResponseFormat verifies that a response format reaches the backend
// and the answer decodes into the Go type the schema was built from.
func TestLLM_ResponseFormat(t *testing.T) {
	format, err := llm.NewResponseFormat[cityFacts]("city_facts", "facts about a city")
	if err != nil {
		t.Fatalf("NewResponseFormat: %v", err)
	}

	for name, client := range map[string]llm.LLM{
		"openai":    buildOpenAILLM(),
		"anthropic": buildAnthropicLLM(),
	} {
		t.Run(name, func(t *testing.T) {
			// Ollama's Anthropic-compatible endpoint (0.35) accepts output_config.format
			// and ignores it, answering in free text. That the field is sent is covered
			// by the anthropic unit tests; here it needs the real API.
			if name == "anthropic" && anthropicBaseURL() == defaultAnthropicURL {
				t.Skip("Ollama's Anthropic endpoint ignores structured output; set ANTHROPIC_BASE_URL to the real API")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			messages := []llm.Message{
				llm.UserMessage(llm.Text("Which country is Paris in? Answer with the city and its country.")),
			}

			result, execErr := client.Execute(ctx, messages, llm.WithResponseFormat(format))
			if execErr != nil {
				t.Fatalf("Execute: %v", execErr)
			}

			text := result.Message.TextContent()

			var facts cityFacts
			if jsonErr := json.Unmarshal([]byte(text), &facts); jsonErr != nil {
				t.Fatalf("answer is not the requested JSON: %v (text: %q)", jsonErr, text)
			}

			if !strings.Contains(strings.ToLower(facts.Country), "france") {
				t.Fatalf("country = %q, want France (answer: %q)", facts.Country, text)
			}
		})
	}
}

// TestLLM_ForcedTool verifies that forcing a tool makes the model call it even
// for a prompt it could answer directly.
func TestLLM_ForcedTool(t *testing.T) {
	type input struct {
		Word string `json:"word"`
	}

	tool, err := llm.NewTool("shout", "Return a word in upper case.", func(_ context.Context, in input) (string, error) {
		return strings.ToUpper(in.Word), nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	for name, client := range map[string]llm.LLM{
		"openai":    buildOpenAILLM(),
		"anthropic": buildAnthropicLLM(),
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			messages := []llm.Message{llm.UserMessage(llm.Text("Say hello."))}

			result, execErr := client.Execute(ctx, messages, llm.WithTools(tool), llm.WithForcedTool("shout"))
			if execErr != nil {
				t.Fatalf("Execute: %v", execErr)
			}

			calls := result.Message.ToolCalls
			if len(calls) == 0 || calls[0].Function.Name != "shout" {
				t.Fatalf("tool calls = %+v, want a call to shout", calls)
			}
		})
	}
}
