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

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	tracejsonfile "github.com/henomis/phero/v2/trace/jsonfile"
	tracetext "github.com/henomis/phero/v2/trace/text"
)

// TestTraceJSONFile_WritesSpans verifies that running an agent with the NDJSON
// processor produces a trace file with the run and model call spans.
func TestTraceJSONFile_WritesSpans(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tracePath := t.TempDir() + "/trace.ndjson"

	proc, err := tracejsonfile.New(tracePath)
	if err != nil {
		t.Fatalf("tracejsonfile.New: %v", err)
	}

	p := trace.NewProvider(trace.WithProcessor(proc))

	llmClient := buildOpenAILLM()
	a, err := agent.New(llmClient, "json-traced-agent", "You are a concise assistant.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	a.SetTracing(p)

	result, err := a.Run(ctx, llm.Text("Say hello in one short sentence."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Agent response: %q", result.TextContent())

	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("os.ReadFile: %v", err)
	}

	content := string(data)
	for _, want := range []string{`"kind":"invoke_agent"`, `"kind":"chat"`, `"stopReason":"final"`} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %s in trace file", want)
		}
	}
}

// TestTraceText_WritesReadableOutput verifies that the text processor writes
// human-readable output lines for an agent run.
func TestTraceText_WritesReadableOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var buf bytes.Buffer

	llmClient := buildOpenAILLM()
	a, err := agent.New(llmClient, "text-traced-agent", "You are a concise assistant.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	a.SetTracing(trace.NewProvider(trace.WithProcessor(tracetext.New(&buf))))

	result, err := a.Run(ctx, llm.Text("Respond with a short greeting."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Agent response: %q", result.TextContent())

	output := buf.String()
	t.Logf("Trace output:\n%s", output)

	for _, want := range []string{"invoke_agent text-traced-agent", "chat", "stop=final"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in text trace", want)
		}
	}
}

// TestTraceContextProvider verifies that an agent without its own provider
// uses the one carried by the context.
func TestTraceContextProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var buf bytes.Buffer

	llmClient := buildOpenAILLM()
	a, err := agent.New(llmClient, "ctx-traced-agent", "You are a concise assistant.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	ctx = trace.WithProvider(ctx, trace.NewProvider(trace.WithProcessor(tracetext.New(&buf))))

	result, err := a.Run(ctx, llm.Text("Say only hi."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Agent response: %q", result.TextContent())

	if !strings.Contains(buf.String(), "ctx-traced-agent") {
		t.Error("expected agent name in trace output")
	}
}
