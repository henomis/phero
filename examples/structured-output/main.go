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

// Command structured-output demonstrates agent.RunTyped, which returns the
// agent's final answer decoded into a Go struct instead of free-form text.
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

// Recipe is the structured shape we want the agent to return.
type Recipe struct {
	Name        string   `json:"name" jsonschema:"description=The name of the dish"`
	Servings    int      `json:"servings" jsonschema:"description=How many people it serves"`
	Ingredients []string `json:"ingredients" jsonschema:"description=The list of ingredients"`
	Steps       []string `json:"steps" jsonschema:"description=The ordered preparation steps"`
}

func main() {
	ctx := context.Background()
	llmClient := buildLLMFromEnv()

	a, err := agent.New(
		llmClient,
		"Chef",
		"You are a concise chef. Produce simple, practical recipes.",
	)
	if err != nil {
		panic(err)
	}

	recipe, result, err := agent.RunTyped[Recipe](ctx, a, llm.Text("Give me a quick recipe for pancakes."))
	if err != nil {
		panic(err)
	}

	fmt.Printf("Dish: %s (serves %d)\n", recipe.Name, recipe.Servings)
	fmt.Printf("Ingredients: %s\n", strings.Join(recipe.Ingredients, ", "))
	fmt.Println("Steps:")
	for i, step := range recipe.Steps {
		fmt.Printf("  %d. %s\n", i+1, step)
	}

	if s := result.Summary; s != nil {
		fmt.Printf("\n(iterations=%d llm_calls=%d cost=$%.4f)\n", s.Iterations, s.LLMCalls, s.Usage.CostUSD)
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
