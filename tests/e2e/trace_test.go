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
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/trace"
	tracejsonfile "github.com/henomis/phero/trace/jsonfile"
	tracetext "github.com/henomis/phero/trace/text"
)

// TestTraceJSONFile_WritesEvents verifies that running an agent with the NDJSON
// tracer produces a non-empty trace file containing known event types.
func TestTraceJSONFile_WritesEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tracePath := t.TempDir() + "/trace.ndjson"

	tracer, err := tracejsonfile.New(tracePath)
	if err != nil {
		t.Fatalf("tracejsonfile.New: %v", err)
	}
	defer func() {
		if cerr := tracer.Close(); cerr != nil {
			t.Logf("tracer.Close: %v", cerr)
		}
	}()

	llmClient := buildOpenAILLM()
	a, err := agent.New(llmClient, "json-traced-agent", "You are a concise assistant.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	a.SetTracer(tracer)
	ctx = trace.WithTracer(ctx, tracer)

	result, err := a.Run(ctx, llm.Text("Say hello in one short sentence."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Agent response: %q", result.TextContent())

	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty trace file")
	}

	content := string(data)
	if !strings.Contains(content, "AgentStart") {
		t.Error("expected AgentStart event in trace file")
	}
	if !strings.Contains(content, "LLMRequest") {
		t.Error("expected LLMRequest event in trace file")
	}
	if !strings.Contains(content, "AgentEnd") {
		t.Error("expected AgentEnd event in trace file")
	}
}

// TestTraceText_WritesReadableOutput verifies that the text tracer writes
// human-readable output lines for an agent run.
func TestTraceText_WritesReadableOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var buf bytes.Buffer
	tracer := tracetext.New(&buf)

	llmClient := buildOpenAILLM()
	a, err := agent.New(llmClient, "text-traced-agent", "You are a concise assistant.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	a.SetTracer(tracer)
	ctx = trace.WithTracer(ctx, tracer)

	result, err := a.Run(ctx, llm.Text("Respond with a short greeting."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Agent response: %q", result.TextContent())

	output := buf.String()
	if strings.TrimSpace(output) == "" {
		t.Fatal("expected non-empty text trace output")
	}

	t.Logf("Trace output:\n%s", output)

	if !strings.Contains(output, "AgentStart") {
		t.Error("expected AgentStart in text trace")
	}
	if !strings.Contains(output, "AgentEnd") {
		t.Error("expected AgentEnd in text trace")
	}
}

// TestTraceContextPropagation verifies that the tracer set on the agent
// is used during agent execution (the agent field takes priority over the
// context value which it overwrites internally).
func TestTraceContextPropagation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var buf bytes.Buffer
	tracer := tracetext.New(&buf)

	llmClient := buildOpenAILLM()
	a, err := agent.New(llmClient, "ctx-traced-agent", "You are a concise assistant.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	// The agent stores its own tracer and overwrites the context tracer on Run.
	// SetTracer is the correct way to inject a tracer; the context value is used
	// by nested agents spawned during hand-off.
	a.SetTracer(tracer)

	result, err := a.Run(ctx, llm.Text("Say only hi."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Agent response: %q", result.TextContent())

	output := buf.String()
	if strings.TrimSpace(output) == "" {
		t.Fatal("expected trace output via context tracer")
	}

	if !strings.Contains(output, "ctx-traced-agent") {
		t.Error("expected agent name in trace output")
	}
}
