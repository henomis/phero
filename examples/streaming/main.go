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

// Command streaming demonstrates Agent.RunStream, which streams the agent's
// progress as a sequence of events (text deltas, tool calls, and a final result)
// instead of returning only the buffered answer.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
)

func main() {
	ctx := context.Background()
	llmClient := buildLLMFromEnv()

	a, err := agent.New(
		llmClient,
		"Storyteller",
		"You are a concise storyteller. Answer in two or three short sentences.",
	)
	if err != nil {
		panic(err)
	}

	fmt.Print("Agent: ")

	for ev, err := range a.RunStream(ctx, llm.Text("Tell me a tiny story about a brave gopher.")) {
		if err != nil {
			panic(err)
		}

		switch ev.Type {
		case agent.EventTextDelta:
			// Print tokens as they arrive.
			fmt.Print(ev.TextDelta)
		case agent.EventToolCall:
			fmt.Printf("\n[calling tool %s(%s)]\n", ev.ToolName, ev.ToolArgs)
		case agent.EventToolResult:
			fmt.Printf("\n[tool %s -> %s]\n", ev.ToolName, ev.ToolResult)
		case agent.EventDone:
			fmt.Println()

			if s := ev.Result.Summary; s != nil {
				fmt.Printf("(llm_calls=%d tokens=%d/%d cost=$%.4f)\n",
					s.LLMCalls, s.Usage.InputTokens, s.Usage.OutputTokens, s.Usage.CostUSD)
			}
		}
	}
}

func buildLLMFromEnv() llm.LLM {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))

	// If no key and no base URL are set, assume a local OpenAI-compatible server (e.g. Ollama).
	if apiKey == "" && baseURL == "" {
		baseURL = openai.OllamaBaseURL
	}

	if model == "" {
		if baseURL == openai.OllamaBaseURL && apiKey == "" {
			model = "gpt-oss:20b-cloud"
		} else {
			model = openai.DefaultModel
		}
	}

	opts := []openai.Option{openai.WithModel(model)}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}

	return openai.New(apiKey, opts...)
}
