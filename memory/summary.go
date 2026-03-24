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

	"github.com/henomis/phero/llm"
)

//nolint:misspell // accepted as is
const summaryPrompt = `# Role
You are a Memory Synthesis Module for an AI Agent. Your goal is to condense the provided conversation history into a structured "State Snapshot" while preserving crucial details for future reasoning.

# Task
Analyze the dialogue below and generate a concise summary based on these four pillars:

1. **Entities & Facts:** List key people, technical stacks, or specific data points mentioned.
2. **User Preferences:** Explicit or implicit likes, dislikes, and stylistic requirements (e.g., "Prefers Go over Python," "Dislikes verbose explanations").
3. **Current Progress:** What was achieved in this window? What problems were solved?
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
const SummarySystemMessagePrefix = "Summary of previous conversation:\n"

// ClampSummarySize validates and normalises a (summarizeThreshold, summarySize)
// pair for use by WithSummarization options across all memory backends.
//
// Rules applied in order:
//  1. If summarySize is zero, derive it as summarizeThreshold/2 (minimum 1).
//  2. If summarySize >= summarizeThreshold, cap it at summarizeThreshold-1
//     (minimum 1) to prevent an infinite summarization loop.
func ClampSummarySize(summarizeThreshold, summarySize uint) uint {
	if summarySize == 0 && summarizeThreshold > 0 {
		summarySize = summarizeThreshold / 2
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
func FormatSummaryPrompt(conversation []llm.Message) llm.Message {
	var formatted string
	for _, msg := range conversation {
		formatted += "## " + msg.Role + "\n" + msg.Content + "\n\n"
	}

	return llm.Message{
		Role:    llm.ChatMessageRoleUser,
		Content: fmt.Sprintf(summaryPrompt, formatted),
	}
}
