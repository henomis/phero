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

package llm_test

import (
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
)

func TestEstimateTokens(t *testing.T) {
	msgs := []llm.Message{
		llm.UserMessage(llm.Text(strings.Repeat("a", 400))),
		llm.AssistantMessage(nil, llm.ToolCall{ID: "c", Function: llm.FunctionCall{Name: "echo", Arguments: strings.Repeat("b", 40)}}),
		llm.ToolResultMessage("c", llm.Text(strings.Repeat("c", 80))),
	}

	// 400/4 + 40/4 + 80/4 = 130, plus an overhead per message.
	if got := llm.EstimateTokens(msgs); got < 130 || got > 160 {
		t.Fatalf("EstimateTokens = %d, want about 130 plus overhead", got)
	}
}

func TestSanitizeToolName(t *testing.T) {
	for name, want := range map[string]string{
		"files.read":            "files_read",
		"my-tool_2":             "my-tool_2",
		"":                      "",
		strings.Repeat("x", 70): strings.Repeat("x", llm.MaxToolNameLength),
	} {
		if got := llm.SanitizeToolName(name); got != want {
			t.Errorf("SanitizeToolName(%q) = %q, want %q", name, got, want)
		}
	}
}
