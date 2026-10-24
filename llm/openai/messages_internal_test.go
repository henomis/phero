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

package openai

import (
	"testing"

	"github.com/sashabaranov/go-openai"

	"github.com/henomis/phero/v2/llm"
)

// TestMessagesToOpenAI_ToolImagesMoveToUserMessage checks that images in tool
// results, which Chat Completions rejects, are sent in one user message after
// the run of tool messages, and that the tool messages keep their text.
func TestMessagesToOpenAI_ToolImagesMoveToUserMessage(t *testing.T) {
	img := llm.ImageBase64("image/png", "cG5n")
	msgs := []llm.Message{
		llm.UserMessage(llm.Text("look")),
		llm.AssistantMessage(nil,
			llm.ToolCall{ID: "a", Function: llm.FunctionCall{Name: "shot", Arguments: "{}"}},
			llm.ToolCall{ID: "b", Function: llm.FunctionCall{Name: "read", Arguments: "{}"}},
		),
		llm.ToolResultMessage("a", img),
		llm.ToolResultMessage("b", llm.Text("file text")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("done")}),
	}

	out := messagesToOpenAI(msgs)

	roles := make([]string, len(out))
	for i, m := range out {
		roles[i] = m.Role
	}

	want := []string{llm.RoleUser, llm.RoleAssistant, llm.RoleTool, llm.RoleTool, llm.RoleUser, llm.RoleAssistant}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}

	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
	}

	for _, m := range out[2:4] {
		for _, p := range m.MultiContent {
			if p.Type == openai.ChatMessagePartTypeImageURL {
				t.Fatalf("tool message %s still has an image", m.ToolCallID)
			}
		}

		if m.Content == "" && len(m.MultiContent) == 0 {
			t.Fatalf("tool message %s has no content", m.ToolCallID)
		}
	}

	user := out[4].MultiContent
	if len(user) != 2 || user[1].ImageURL == nil || user[1].ImageURL.URL != "data:image/png;base64,cG5n" {
		t.Fatalf("user message = %+v, want the tool's image", user)
	}
}

// TestMessagesToOpenAI_ToolImagesAtEnd checks that images of the last tool
// messages are still sent when no message follows them.
func TestMessagesToOpenAI_ToolImagesAtEnd(t *testing.T) {
	msgs := []llm.Message{
		llm.AssistantMessage(nil, llm.ToolCall{ID: "a", Function: llm.FunctionCall{Name: "shot", Arguments: "{}"}}),
		llm.ToolResultMessage("a", llm.Text("screen"), llm.ImageURL("https://example.com/a.png")),
	}

	out := messagesToOpenAI(msgs)
	if len(out) != 3 || out[2].Role != llm.RoleUser || out[1].Content != "screen" {
		t.Fatalf("out = %+v", out)
	}
}

func TestMessageFromOpenAI_KeepsReasoningContent(t *testing.T) {
	msg := messageFromOpenAI(openai.ChatCompletionMessage{
		Role: llm.RoleAssistant, Content: "Answer", ReasoningContent: "Thinking.",
	})

	if msg.ReasoningContent() != "Thinking." || msg.TextContent() != "Answer" || msg.Parts[0].Type != llm.ContentTypeReasoning {
		t.Fatalf("message = %+v, want the reasoning part first and the text", msg)
	}
}

// TestMessagesToOpenAI_SkipsReasoningOnlyAnswer checks that an assistant
// message with nothing to send but reasoning is left out, rather than sent
// with no content.
func TestMessagesToOpenAI_SkipsReasoningOnlyAnswer(t *testing.T) {
	out := messagesToOpenAI([]llm.Message{
		llm.UserMessage(llm.Text("q")),
		llm.AssistantMessage([]llm.ContentPart{llm.Reasoning("Thinking.", "")}),
		llm.UserMessage(llm.Text("again")),
	})

	if len(out) != 2 || out[0].Role != llm.RoleUser || out[1].Role != llm.RoleUser {
		t.Fatalf("out = %+v, want the two user messages", out)
	}
}
