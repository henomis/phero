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
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/tool/bash"
)

// TestBash_StreamsInRunStream verifies that a bash command's output reaches
// the output handler while the command runs, before the agent's
// EventToolResult for that call.
func TestBash_StreamsInRunStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	var (
		mu     sync.Mutex
		chunks []time.Time
	)

	bashTool, err := bash.New(
		bash.WithWorkingDirectory(t.TempDir()),
		bash.WithEnvAllowlist(bash.DefaultEnvAllowlist...),
		bash.WithOutputHandler(func(ctx context.Context, _ []byte) {
			if _, ok := llm.ToolCallFromContext(ctx); !ok {
				t.Error("output handler: no tool call in context")
			}

			mu.Lock()
			defer mu.Unlock()

			chunks = append(chunks, time.Now())
		}),
	)
	if err != nil {
		t.Fatalf("bash.New: %v", err)
	}
	defer func() { _ = bashTool.Close() }()

	a, err := agent.New(buildOpenAILLM(), "runner",
		"You run shell commands with the bash tool exactly as the user writes them, then report the output.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	if err = a.AddTool(bashTool.Tool()); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	const command = "for i in 1 2 3; do echo $i; sleep 1; done"

	var called bool

	for ev, err := range a.RunStream(ctx, llm.Text("Run this command with the bash tool: "+command)) {
		if err != nil {
			t.Fatalf("RunStream: %v", err)
		}

		if ev.Type != agent.EventToolResult || ev.ToolName != "bash" || called {
			continue
		}

		called = true
		resultAt := time.Now()

		mu.Lock()
		got := slices.Clone(chunks)
		mu.Unlock()

		t.Logf("bash result after %d chunks: %q", len(got), ev.ToolResult)

		// The command prints for about 3 s: its first line must have arrived
		// well before the result.
		if len(got) < 2 || resultAt.Sub(got[0]) < time.Second {
			t.Fatalf("got %d chunks, first %v before the result: want output streamed while the command ran",
				len(got), resultAt.Sub(got[0]))
		}
	}

	if !called {
		t.Fatal("the model never called bash")
	}
}
