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
