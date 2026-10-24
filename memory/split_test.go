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

package memory_test

import (
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
)

// toolSession returns user, assistant(tool call a, b), tool a, tool b, assistant.
func toolSession() []llm.Message {
	return []llm.Message{
		llm.UserMessage(llm.Text("fix the bug")),
		llm.AssistantMessage(nil,
			llm.ToolCall{ID: "a", Function: llm.FunctionCall{Name: "read", Arguments: `{"path":"main.go"}`}},
			llm.ToolCall{ID: "b", Function: llm.FunctionCall{Name: "bash", Arguments: `{"command":"go test ./..."}`}},
		),
		llm.ToolResultMessage("a", llm.Text("package main")),
		{Role: llm.RoleTool, ToolCallID: "b", ToolError: true, Parts: []llm.ContentPart{llm.Text("FAIL")}},
		llm.AssistantMessage([]llm.ContentPart{llm.Text("done")}),
	}
}

func TestSplitPoint(t *testing.T) {
	msgs := toolSession()

	cases := map[int]int{
		-1: 0,
		0:  0,
		1:  1, // before the assistant message: the pair stays on the kept side
		2:  4, // between the call and its results: moved past both results
		3:  4,
		4:  4,
		5:  5,
		9:  5,
	}

	for i, want := range cases {
		if got := memory.SplitPoint(msgs, i); got != want {
			t.Errorf("SplitPoint(%d) = %d, want %d", i, got, want)
		}
	}
}

func TestTrimOrphanToolResults(t *testing.T) {
	msgs := toolSession()

	if got := memory.TrimOrphanToolResults(msgs[2:]); len(got) != 1 || got[0].Role != llm.RoleAssistant {
		t.Fatalf("got %+v, want only the final assistant message", got)
	}

	if got := memory.TrimOrphanToolResults(msgs); len(got) != len(msgs) {
		t.Fatalf("trimmed a slice without orphans: %d messages", len(got))
	}
}

func TestFormatSummaryPrompt_IncludesToolCalls(t *testing.T) {
	prompt := memory.FormatSummaryPrompt(toolSession()).TextContent()

	for _, want := range []string{
		`tool call a: read({"path":"main.go"})`,
		`tool call b: bash({"command":"go test ./..."})`,
		"## tool result (call a)\npackage main",
		"## tool error (call b)\nFAIL",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not contain %q", want)
		}
	}
}

func TestFormatSummaryPrompt_ClipsLongToolResults(t *testing.T) {
	long := strings.Repeat("x", 10000)
	msgs := []llm.Message{llm.ToolResultMessage("a", llm.Text(long)), llm.UserMessage(llm.Text(long))}

	prompt := memory.FormatSummaryPrompt(msgs).TextContent()
	if strings.Count(prompt, "[…]") != 1 || !strings.Contains(prompt, long) {
		t.Fatal("want the tool result clipped and the user text kept whole")
	}
}
