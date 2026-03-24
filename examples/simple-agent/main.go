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

// CalculatorInput defines the parameters for the calculator tool.
type CalculatorInput struct {
	Operation string  `json:"operation" jsonschema:"description=The operation to perform: add subtract multiply or divide,enum=add,enum=subtract,enum=multiply,enum=divide"`
	A         float64 `json:"a" jsonschema:"description=The first number"`
	B         float64 `json:"b" jsonschema:"description=The second number"`
}

// CalculatorOutput defines the result returned by the calculator.
type CalculatorOutput struct {
	Result float64 `json:"result" jsonschema:"description=The result of the calculation"`
	Error  string  `json:"error,omitempty" jsonschema:"description=Error message if the operation failed"`
}

// calculate performs basic arithmetic operations.
func calculate(_ context.Context, input *CalculatorInput) (*CalculatorOutput, error) {
	fmt.Printf("Tool called with input: %+v\n", input)
	if input == nil {
		return &CalculatorOutput{Error: "missing input"}, nil
	}

	switch input.Operation {
	case "add":
		return &CalculatorOutput{Result: input.A + input.B}, nil
	case "subtract":
		return &CalculatorOutput{Result: input.A - input.B}, nil
	case "multiply":
		return &CalculatorOutput{Result: input.A * input.B}, nil
	case "divide":
		if input.B == 0 {
			return &CalculatorOutput{Error: "division by zero"}, nil
		}
		return &CalculatorOutput{Result: input.A / input.B}, nil
	default:
		return &CalculatorOutput{Error: "unknown operation"}, nil
	}
}

func main() {
	ctx := context.Background()

	// Build LLM client from environment variables
	llmClient, llmInfo := buildLLMFromEnv()

	// Create the calculator tool from our Go function
	calcTool, err := llm.NewTool(
		"calculator",
		"Performs basic arithmetic operations: add, subtract, multiply, divide",
		calculate,
	)
	if err != nil {
		panic(err)
	}

	// Create an agent with a simple system prompt
	a, err := agent.New(
		llmClient,
		"Math Assistant",
		"You are a helpful math assistant. Use the calculator tool to perform calculations accurately.",
	)
	if err != nil {
		panic(err)
	}

	// Add the calculator tool to the agent
	if err := a.AddTool(calcTool); err != nil {
		panic(err)
	}

	// Run the agent with a user request
	userRequest := "If I have 15 apples and give away 7, then buy 23 more, how many do I have?"
	fmt.Printf("LLM: %s\n", llmInfo)
	fmt.Printf("User: %s\n\n", userRequest)

	response, err := a.Run(ctx, userRequest)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Agent: %s\n", response)
}

func buildLLMFromEnv() (llm.LLM, string) {
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
	client := openai.New(apiKey, opts...)

	info := fmt.Sprintf("model=%s base_url=%s", model, baseURL)
	if baseURL == "" {
		info = fmt.Sprintf("model=%s", model)
	}

	return client, info
}
