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

func TestSummaryMessages(t *testing.T) {
	task := llm.UserMessage(llm.Text("fix the bug"))

	got := memory.SummaryMessages(memory.Snapshot{Summary: "did things", Pinned: []llm.Message{task}})

	if len(got) != 2 || got[0].Role != llm.RoleUser || got[1].TextContent() != "fix the bug" {
		t.Fatalf("SummaryMessages = %+v, want the summary as a user message, then the pinned task", got)
	}

	if !strings.Contains(got[0].TextContent(), "did things") {
		t.Fatalf("summary message = %q", got[0].TextContent())
	}
}

func TestSameSnapshot(t *testing.T) {
	a, b := &memory.Snapshot{UpTo: "4"}, &memory.Snapshot{Summary: "other", UpTo: "4"}

	if !memory.SameSnapshot(nil, nil) || !memory.SameSnapshot(a, b) {
		t.Fatal("want nil == nil and equal cursors to be the same snapshot")
	}

	if memory.SameSnapshot(a, nil) || memory.SameSnapshot(a, &memory.Snapshot{UpTo: "5"}) {
		t.Fatal("want nil and other cursors to differ")
	}
}

func TestFormatSnapshotPrompt(t *testing.T) {
	conversation := []llm.Message{
		llm.UserMessage(llm.Text("rename Foo")),
		llm.AssistantMessage(nil, llm.ToolCall{ID: "c1", Function: llm.FunctionCall{Name: "edit", Arguments: `{"file_path":"/repo/a.go"}`}}),
		llm.ToolResultMessage("c1", llm.Text("1 replacement")),
	}

	prompt := memory.FormatSnapshotPrompt("- earlier: read the repo", conversation).TextContent()

	for _, want := range []string{
		"- earlier: read the repo",             // the previous summary is updated, not lost
		"edit(", "/repo/a.go", "1 replacement", // tool work is listed
		"## Task", "## Decisions and facts", "## Files and commands", "## User preferences", "## Open items",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}

	if first := memory.FormatSnapshotPrompt("", conversation).TextContent(); !strings.Contains(first, "(none)") {
		t.Error("a first snapshot should say there is no previous summary")
	}
}
