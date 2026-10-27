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
