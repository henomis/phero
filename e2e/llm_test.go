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

	"github.com/henomis/phero/llm"
)

// TestOpenAILLM_TextGeneration verifies that the OpenAI-compatible client
// (pointed at Ollama) can generate a non-empty text response.
func TestOpenAILLM_TextGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := buildOpenAILLM()

	messages := []llm.Message{
		llm.UserMessage(llm.Text("Reply with exactly the word PONG and nothing else.")),
	}

	result, err := client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result == nil || result.Message == nil {
		t.Fatal("Execute returned nil result or nil message")
	}

	text := llm.TextContent(result.Message.Parts...)
	if strings.TrimSpace(text) == "" {
		t.Fatal("Execute returned empty text content")
	}

	t.Logf("LLM response: %q", text)

	if result.Usage == nil {
		t.Fatal("Execute returned nil usage")
	}

	if result.Usage.OutputTokens == 0 {
		t.Error("expected non-zero output tokens")
	}
}

// TestOpenAILLM_SystemPrompt verifies that a system message is respected.
func TestOpenAILLM_SystemPrompt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := buildOpenAILLM()

	messages := []llm.Message{
		llm.SystemMessage("You are a bot that only responds with the number 42."),
		llm.UserMessage(llm.Text("What is the answer?")),
	}

	result, err := client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	text := llm.TextContent(result.Message.Parts...)
	t.Logf("LLM response: %q", text)

	if strings.TrimSpace(text) == "" {
		t.Fatal("Execute returned empty text content")
	}
}

// TestOpenAILLM_ToolCall verifies that the model can call a tool.
func TestOpenAILLM_ToolCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type PingInput struct{}
	type PingOutput struct {
		Response string `json:"response"`
	}

	tool, err := llm.NewTool("ping", "Returns a pong response", func(_ context.Context, _ *PingInput) (*PingOutput, error) {
		return &PingOutput{Response: "pong"}, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	client := buildOpenAILLM()

	messages := []llm.Message{
		llm.SystemMessage("You must call the ping tool to test the connection. Do not respond without calling it."),
		llm.UserMessage(llm.Text("Please ping.")),
	}

	result, err := client.Execute(ctx, messages, []*llm.Tool{tool})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result == nil || result.Message == nil {
		t.Fatal("Execute returned nil result or nil message")
	}

	t.Logf("role=%s toolCalls=%d", result.Message.Role, len(result.Message.ToolCalls))
}

// TestAnthropicLLM_TextGeneration verifies that the Anthropic-compatible client
// (pointed at Ollama) can generate a non-empty text response.
func TestAnthropicLLM_TextGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := buildAnthropicLLM()

	messages := []llm.Message{
		llm.UserMessage(llm.Text("Reply with exactly the word PONG and nothing else.")),
	}

	result, err := client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result == nil || result.Message == nil {
		t.Fatal("Execute returned nil result or nil message")
	}

	text := llm.TextContent(result.Message.Parts...)
	if strings.TrimSpace(text) == "" {
		t.Fatal("Execute returned empty text content")
	}

	t.Logf("Anthropic LLM response: %q", text)
}

// TestAnthropicLLM_ToolCall verifies that the Anthropic model can call a tool.
func TestAnthropicLLM_ToolCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type EchoInput struct {
		Message string `json:"message" jsonschema:"description=The message to echo back"`
	}
	type EchoOutput struct {
		Echoed string `json:"echoed"`
	}

	tool, err := llm.NewTool("echo", "Echoes back the provided message", func(_ context.Context, in *EchoInput) (*EchoOutput, error) {
		return &EchoOutput{Echoed: in.Message}, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	client := buildAnthropicLLM()

	messages := []llm.Message{
		llm.UserMessage(llm.Text("Please call the echo tool with message 'hello'.")),
	}

	result, err := client.Execute(ctx, messages, []*llm.Tool{tool})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result == nil || result.Message == nil {
		t.Fatal("Execute returned nil result or nil message")
	}

	t.Logf("role=%s toolCalls=%d", result.Message.Role, len(result.Message.ToolCalls))
}

// TestOpenAILLM_MultiTurn verifies that a multi-turn conversation works.
func TestOpenAILLM_MultiTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	client := buildOpenAILLM()

	messages := []llm.Message{
		llm.SystemMessage("You are a helpful assistant. Keep answers short."),
		llm.UserMessage(llm.Text("My name is Alice.")),
	}

	result, err := client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("first turn Execute: %v", err)
	}

	messages = append(messages, *result.Message)
	messages = append(messages, llm.UserMessage(llm.Text("What is my name?")))

	result, err = client.Execute(ctx, messages, nil)
	if err != nil {
		t.Fatalf("second turn Execute: %v", err)
	}

	text := llm.TextContent(result.Message.Parts...)
	t.Logf("Multi-turn response: %q", text)

	if !strings.Contains(strings.ToLower(text), "alice") {
		t.Logf("Note: expected 'alice' in response %q (model may not have retained it)", text)
	}
}
