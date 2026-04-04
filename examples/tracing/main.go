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
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/trace"
	"github.com/henomis/phero/trace/text"
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
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	client := openai.New(apiKey, openai.WithModel("gpt-4o-mini"))

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

	// Attach a text Tracer to see all lifecycle events in the terminal.
	a.SetTracer(text.New(os.Stderr))

	if err := a.AddTool(calculatorTool); err != nil {
		fmt.Fprintf(os.Stderr, "AddTool: %v\n", err)
		os.Exit(1)
	}

	result, err := a.Run(context.Background(), "What is (123 * 456) + (789 / 3)?")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Run: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nResult: %s\n", result.Content)
	printRunSummary(result.Summary)
}

func printRunSummary(summary *trace.RunSummary) {
	if summary == nil {
		return
	}

	fmt.Printf(
		"Run summary: iterations=%d llm_calls=%d tool_calls=%d tokens=%d/%d latency=%s\n",
		summary.Iterations,
		summary.LLMCalls,
		summary.ToolCalls,
		summary.Usage.InputTokens,
		summary.Usage.OutputTokens,
		summary.Latency.Total.Round(time.Millisecond),
	)
}
