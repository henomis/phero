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

package memory

import (
	"fmt"
	"strings"

	"github.com/henomis/phero/v2/llm"
)

//nolint:misspell // accepted as is
const summaryPrompt = `# Role
You are a Memory Synthesis Module for an AI Agent. Your goal is to condense the provided
conversation history into a structured "State Snapshot" while preserving crucial details for
future reasoning.

# Task
Analyze the dialogue below and generate a concise summary based on these four pillars:

1. **Entities & Facts:** List key people, technical stacks, or specific data points mentioned.
2. **User Preferences:** Explicit or implicit likes, dislikes, and stylistic requirements
(e.g., "Prefers Go over Python," "Dislikes verbose explanations").
3. **Current Progress:** What was achieved in this window? What problems were solved?
Include the tool calls that mattered and their outcome: files read or changed, commands
run, errors met.
4. **Open Loops:** What are the pending questions, tasks, or follow-up items the user is expecting?

# Constraints
- Do not use filler phrases like "The user and the AI discussed..."
- Be dense and factual. 
- Use Markdown bullet points.
- If a fact has changed (e.g., User changed their mind), only record the most recent state.

---
# CONVERSATION TO SUMMARIZE:
%s`

// SummarySystemMessagePrefix is the prefix prepended to the generated summary
// when it is stored as a system message in memory.
const (
	SummarySystemMessagePrefix = "Summary of previous conversation:\n"
	summaryDefaultHalve        = 2

	// summaryToolArgsLimit and summaryToolTextLimit cap, in runes, the
	// arguments of a tool call and the text of a tool result in the summary
	// prompt.
	summaryToolArgsLimit = 500
	summaryToolTextLimit = 4000
)

// ClampSummarySize validates and normalises a (summarizeThreshold, summarySize)
// pair for use by WithSummarization options across all memory backends.
//
// Rules applied in order:
//  1. If summarySize is zero, derive it as summarizeThreshold/2 (minimum 1).
//  2. If summarySize >= summarizeThreshold, cap it at summarizeThreshold-1
//     (minimum 1) to prevent an infinite summarization loop.
func ClampSummarySize(summarizeThreshold, summarySize uint) uint {
	if summarySize == 0 && summarizeThreshold > 0 {
		summarySize = summarizeThreshold / summaryDefaultHalve
		if summarySize == 0 {
			summarySize = 1
		}
	}

	if summarySize >= summarizeThreshold && summarizeThreshold > 0 {
		if summarizeThreshold > 1 {
			summarySize = summarizeThreshold - 1
		} else {
			summarySize = 1
		}
	}

	return summarySize
}

// FormatSummaryPrompt builds the summarization request message sent to the LLM
// when a memory backend needs to condense its conversation history. The returned
// message has the user role so that the model treats it as a regular user turn.
//
// Tool calls are listed with their arguments and tool results with their
// text, both cut to a few hundred or thousand characters, so the summary can
// record what the agent did and not only what it said.
func FormatSummaryPrompt(conversation []llm.Message) llm.Message {
	return llm.UserMessage(llm.Text(fmt.Sprintf(summaryPrompt, formatConversation(conversation))))
}

//nolint:misspell // accepted as is
const snapshotPrompt = `# Role
You maintain the working memory of an AI agent in the middle of a long task. Write an updated
summary that lets the agent continue the task without the messages it replaces.

# Task
Update the previous summary (if any) with the new conversation below. Keep everything from the
previous summary that is still true; replace what changed. Use exactly these sections:

## Task
The user's goal and the current sub-task, in their words where it matters.

## Decisions and facts
Decisions taken and why, constraints, and facts learned (APIs, versions, configuration).

## Files and commands
Files read, created or changed (with what changed), commands run and their outcome, errors met
and whether they were fixed.

## User preferences
Explicit or implicit preferences about the work and the answers.

## Open items / next steps
What is left to do, pending questions, and the next step the agent was about to take.

# Constraints
- Be dense and factual. Use Markdown bullet points under each section.
- No filler such as "The user and the assistant discussed".
- If something changed, record only its latest state.

# PREVIOUS SUMMARY
%s

# NEW CONVERSATION
%s`

// FormatSnapshotPrompt builds the request that updates previous, the summary
// of an earlier snapshot (empty for none), with conversation, the messages the
// new snapshot will also stand for. It formats conversation as
// FormatSummaryPrompt does, and asks for fixed sections: task, decisions and
// facts, files and commands, user preferences, open items.
func FormatSnapshotPrompt(previous string, conversation []llm.Message) llm.Message {
	if strings.TrimSpace(previous) == "" {
		previous = "(none)"
	}

	return llm.UserMessage(llm.Text(fmt.Sprintf(snapshotPrompt, previous, formatConversation(conversation))))
}

// formatConversation renders messages for a summary prompt: each message's
// role and text, tool results with their status and call ID, and tool calls
// with their arguments, cut to a few hundred or thousand characters.
func formatConversation(conversation []llm.Message) string {
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

			text = clip(text, summaryToolTextLimit)
		}

		formatted.WriteString("\n" + text + "\n")

		for _, tc := range msg.ToolCalls {
			formatted.WriteString("- tool call " + tc.ID + ": " + tc.Function.Name + "(" +
				clip(tc.Function.Arguments, summaryToolArgsLimit) + ")\n")
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
