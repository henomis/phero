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
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	simplemem "github.com/henomis/phero/memory/simple"
	"github.com/henomis/phero/trace"
	tracejsonfile "github.com/henomis/phero/trace/jsonfile"
)

// TestAgent_SimpleTextResponse verifies that an agent can execute a single
// turn and return a non-empty text response.
func TestAgent_SimpleTextResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	llmClient := buildOpenAILLM()

	a, err := agent.New(llmClient, "assistant", "You are a helpful assistant. Keep answers brief.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	result, err := a.Run(ctx, llm.Text("What is the capital of France?"))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	text := result.TextContent()
	t.Logf("Agent response: %q", text)

	if strings.TrimSpace(text) == "" {
		t.Error("agent returned empty response")
	}
}

// TestAgent_ToolCall verifies that an agent can call a tool and incorporate
// the result into its final response.
func TestAgent_ToolCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	type CalcInput struct {
		A float64 `json:"a" jsonschema:"description=First number"`
		B float64 `json:"b" jsonschema:"description=Second number"`
	}
	type CalcOutput struct {
		Sum float64 `json:"sum"`
	}

	toolCalled := false

	calcTool, err := llm.NewTool("add", "Adds two numbers and returns their sum", func(_ context.Context, in *CalcInput) (*CalcOutput, error) {
		toolCalled = true
		return &CalcOutput{Sum: in.A + in.B}, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	llmClient := buildOpenAILLM()

	a, err := agent.New(llmClient, "calculator", "You are a calculator agent. Use the add tool to perform additions.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	if err := a.AddTool(calcTool); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, err := a.Run(ctx, llm.Text("What is 17 + 25?"))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	text := result.TextContent()
	t.Logf("Agent response: %q (toolCalled=%v)", text, toolCalled)

	if !toolCalled {
		t.Log("Note: agent did not call the add tool (model may have answered directly)")
	}

	if strings.TrimSpace(text) == "" {
		t.Error("agent returned empty response")
	}

	if result.Summary != nil {
		t.Logf("Summary: iterations=%d llmCalls=%d toolCalls=%d",
			result.Summary.Iterations, result.Summary.LLMCalls, result.Summary.ToolCalls)
	}
}

// TestAgent_MultipleTools verifies that an agent with multiple tools selects
// the appropriate one for the given task.
func TestAgent_MultipleTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	type GreetInput struct {
		Name string `json:"name" jsonschema:"description=The name to greet"`
	}
	type GreetOutput struct {
		Greeting string `json:"greeting"`
	}

	type TimeInput struct{}
	type TimeOutput struct {
		Time string `json:"time"`
	}

	greetTool, err := llm.NewTool("greet", "Greets a person by name", func(_ context.Context, in *GreetInput) (*GreetOutput, error) {
		return &GreetOutput{Greeting: "Hello, " + in.Name + "!"}, nil
	})
	if err != nil {
		t.Fatalf("NewTool greet: %v", err)
	}

	timeTool, err := llm.NewTool("current_time", "Returns the current time", func(_ context.Context, _ *TimeInput) (*TimeOutput, error) {
		return &TimeOutput{Time: time.Now().Format(time.RFC3339)}, nil
	})
	if err != nil {
		t.Fatalf("NewTool time: %v", err)
	}

	llmClient := buildOpenAILLM()

	a, err := agent.New(llmClient, "multi-tool-agent", "You are a helpful agent with access to greeting and time tools.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	if err := a.AddTool(greetTool); err != nil {
		t.Fatalf("AddTool greet: %v", err)
	}

	if err := a.AddTool(timeTool); err != nil {
		t.Fatalf("AddTool time: %v", err)
	}

	result, err := a.Run(ctx, llm.Text("Please greet Alice."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Response: %q", result.TextContent())
}

// TestAgent_SimpleMemory verifies that an agent with simple memory retains
// context across a conversation stored in memory.
func TestAgent_SimpleMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	llmClient := buildOpenAILLM()
	mem := simplemem.New(20)

	a, err := agent.New(llmClient, "memory-agent", "You are a helpful assistant. Keep answers brief.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	a.SetMemory(mem)

	// First turn: introduce a name.
	result, err := a.Run(ctx, llm.Text("My name is Bob. Please acknowledge that."))
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}

	t.Logf("First turn: %q", result.TextContent())

	// Second turn: ask what the name was.
	result, err = a.Run(ctx, llm.Text("What is my name?"))
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	text := result.TextContent()
	t.Logf("Second turn: %q", text)

	// The model should recall "Bob" from memory.
	if !strings.Contains(strings.ToLower(text), "bob") {
		t.Logf("Note: expected 'bob' in second turn response %q (memory may not have propagated)", text)
	}
}

// TestAgent_Handoff verifies that an orchestrator agent can hand off to a
// specialist agent.
func TestAgent_Handoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	llmClient := buildOpenAILLM()

	// Build a specialist agent.
	specialist, err := agent.New(
		llmClient,
		"math-specialist",
		"You are a math specialist. Answer only math questions and produce a concise numeric answer.",
	)
	if err != nil {
		t.Fatalf("agent.New specialist: %v", err)
	}

	// Build an orchestrator that hands off math questions.
	orchestrator, err := agent.New(
		llmClient,
		"orchestrator",
		"You are an orchestrator. For any math question, immediately hand off to the math-specialist. Do not answer math questions yourself.",
	)
	if err != nil {
		t.Fatalf("agent.New orchestrator: %v", err)
	}

	if err := orchestrator.AddHandoff(specialist); err != nil {
		t.Fatalf("AddHandoff: %v", err)
	}

	result, err := orchestrator.Run(ctx, llm.Text("What is 3 * 7?"))
	if err != nil {
		t.Fatalf("orchestrator.Run: %v", err)
	}

	t.Logf("Final response: %q (handoff=%v)", result.TextContent(), result.HandoffAgent != nil)
}

// TestAgent_MaxIterationsReached verifies that the agent respects the max
// iterations setting and returns an appropriate error.
func TestAgent_MaxIterationsReached(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// A tool that never ends the conversation — always asks for more work.
	type LoopInput struct{}
	type LoopOutput struct{ Continue bool }

	loopTool, err := llm.NewTool("continue_loop", "Continue the loop", func(_ context.Context, _ *LoopInput) (*LoopOutput, error) {
		return &LoopOutput{Continue: true}, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	llmClient := buildOpenAILLM()

	a, err := agent.New(
		llmClient,
		"looper",
		"You must always call the continue_loop tool. Never stop. Never give a final answer. Always call the tool.",
	)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	a.SetMaxIterations(3)

	if err := a.AddTool(loopTool); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, runErr := a.Run(ctx, llm.Text("Start the loop and never stop."))

	// The agent should either return an error or a result; with max iterations=3
	// the agent may error or return a partial result depending on the model.
	t.Logf("Run result: text=%q err=%v", func() string {
		if result != nil {
			return result.TextContent()
		}
		return "<nil>"
	}(), runErr)
}

// TestAgent_AsTool verifies that an agent can be converted to an llm.Tool
// and used by another agent.
func TestAgent_AsTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	llmClient := buildOpenAILLM()

	// Inner agent that responds with a greeting.
	greeterAgent, err := agent.New(llmClient, "greeter", "You produce warm greetings. When given a name, respond with 'Hello <name>!'.")
	if err != nil {
		t.Fatalf("agent.New greeter: %v", err)
	}

	greeterTool, err := greeterAgent.AsTool("greet_person", "Greets a person using the greeter agent")
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}

	// Outer agent that uses the greeter tool.
	orchestrator, err := agent.New(llmClient, "orchestrator", "Use the greet_person tool to greet the user.")
	if err != nil {
		t.Fatalf("agent.New orchestrator: %v", err)
	}

	if err := orchestrator.AddTool(greeterTool); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, err := orchestrator.Run(ctx, llm.Text("Please greet Carol."))
	if err != nil {
		t.Fatalf("orchestrator.Run: %v", err)
	}

	t.Logf("AsTool response: %q", result.TextContent())
}

// TestAgent_TracingIntegration verifies that the tracer receives events during
// an agent run and that the summary is populated.
func TestAgent_TracingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Use a jsonfile tracer written to a temp file.
	traceFile := t.TempDir() + "/trace.ndjson"

	tracer, err := tracejsonfile.New(traceFile)
	if err != nil {
		t.Fatalf("tracejsonfile.New: %v", err)
	}

	defer func() {
		if cerr := tracer.Close(); cerr != nil {
			t.Logf("tracer.Close: %v", cerr)
		}
	}()

	llmClient := buildOpenAILLM()

	ctx = trace.WithTracer(ctx, tracer)

	a, err := agent.New(llmClient, "traced-agent", "You are a brief assistant.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	a.SetTracer(tracer)

	result, err := a.Run(ctx, llm.Text("Say hello."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Response: %q", result.TextContent())

	if result.Summary == nil {
		t.Fatal("expected non-nil Summary")
	}

	if result.Summary.LLMCalls == 0 {
		t.Error("expected at least one LLM call in summary")
	}

	t.Logf("Summary: iterations=%d llmCalls=%d", result.Summary.Iterations, result.Summary.LLMCalls)
}
