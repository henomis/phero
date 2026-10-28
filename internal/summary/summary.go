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

// Package summary formats a conversation for the prompts that ask an LLM to
// summarize it, shared by the memory backends and the agent's compactor.
package summary

import (
	"strings"

	"github.com/henomis/phero/v2/llm"
)

// toolArgsLimit and toolTextLimit cap, in runes, the arguments of a tool call
// and the text of a tool result in a summary prompt.
const (
	toolArgsLimit = 500
	toolTextLimit = 4000
)

// Conversation renders messages for a summary prompt: each message's
// role and text, tool results with their status and call ID, and tool calls
// with their arguments, cut to a few hundred or thousand characters.
func Conversation(conversation []llm.Message) string {
	var formatted strings.Builder

	for _, msg := range conversation {
		text := msg.TextContent()

		formatted.WriteString("## " + msg.Role)

		if msg.Role == llm.RoleTool {
			status := "result"
			if msg.ToolError {
				status = "error"
			}

			formatted.WriteString(" " + status + " (call " + msg.ToolCallID + ")")

			text = clip(text, toolTextLimit)
		}

		formatted.WriteString("\n" + text + "\n")

		for _, tc := range msg.ToolCalls {
			formatted.WriteString("- tool call " + tc.ID + ": " + tc.Function.Name + "(" +
				clip(tc.Function.Arguments, toolArgsLimit) + ")\n")
		}

		formatted.WriteString("\n")
	}

	return formatted.String()
}

// clip returns s cut to at most limit runes, marking the cut.
func clip(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit]) + " […]"
}
