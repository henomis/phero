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

// Package main demonstrates tracing with a Phero agent.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/openai"
	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/text"
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

func calculate(_ context.Context, input *CalculatorInput) (*CalculatorOutput, error) {
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
		return &CalculatorOutput{Error: "unknown operation: " + input.Operation}, nil
	}
}

func main() {
	// Build LLM client from environment variables
	client, llmInfo := buildLLMFromEnv()
	fmt.Fprintf(os.Stderr, "LLM: %s\n\n", llmInfo)

	calculatorTool, err := llm.NewTool("calculator", "Performs basic arithmetic", calculate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm.NewTool: %v\n", err)
		os.Exit(1)
	}

	a, err := agent.New(client, "math-agent", "You are a helpful math assistant. Use the calculator tool to solve problems.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent.New: %v\n", err)
		os.Exit(1)
	}

	// Print the span tree of the run (agent, LLM calls, tools) to the terminal.
	a.SetTracing(trace.NewProvider(trace.WithProcessor(text.New(os.Stderr))))

	if err := a.AddTool(calculatorTool); err != nil {
		fmt.Fprintf(os.Stderr, "AddTool: %v\n", err)
		os.Exit(1)
	}

	result, err := a.Run(context.Background(), llm.Text("What is (123 * 456) + (789 / 3)?"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Run: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nResult: %s\n", result.TextContent())
	printRunSummary(result.Summary)
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

func printRunSummary(summary *trace.RunSummary) {
	if summary == nil {
		return
	}

	fmt.Printf(
		"Run summary: stop=%s iterations=%d llm_calls=%d tool_calls=%d tokens=%d/%d cost=$%.4f latency=%s\n",
		summary.StopReason,
		summary.Iterations,
		summary.LLMCalls,
		summary.ToolCalls,
		summary.Usage.InputTokens,
		summary.Usage.OutputTokens,
		summary.Usage.CostUSD,
		summary.Latency.Total.Round(time.Millisecond),
	)
}
