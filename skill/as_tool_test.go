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

package skill

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/henomis/phero/llm"
	openai "github.com/sashabaranov/go-openai"
)

type stubLLM struct {
	execute func(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error)
}

func (s stubLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	return s.execute(ctx, messages, tools)
}

func TestSkillAsToolRegistersOnlyAllowedDefaultTools(t *testing.T) {
	t.Helper()

	var capturedToolNames []string
	client := stubLLM{
		execute: func(_ context.Context, _ []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
			capturedToolNames = capturedToolNames[:0]
			for _, tool := range tools {
				capturedToolNames = append(capturedToolNames, tool.Name())
			}

			return &llm.Result{Message: &llm.Message{
				Role:    llm.ChatMessageRoleAssistant,
				Content: "ok",
			}}, nil
		},
	}

	skillTool, err := (&Skill{
		Name:         "skill-tool",
		Description:  "test skill",
		Body:         "system instructions",
		RootPath:     t.TempDir(),
		AllowedTools: toolNameView + " " + toolNameStrReplace,
	}).AsTool(client)
	if err != nil {
		t.Fatalf("AsTool() error = %v", err)
	}

	if _, err := skillTool.Handle(context.Background(), `{"input":"list tools"}`); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	got := strings.Join(capturedToolNames, ",")
	if got != toolNameView+","+toolNameStrReplace {
		t.Fatalf("registered tools = %q, want %q", got, toolNameView+","+toolNameStrReplace)
	}
}

func TestSkillAsToolRejectsNullCreateFileArguments(t *testing.T) {
	client := stubLLM{
		execute: func(_ context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
			if len(messages) >= 2 && messages[len(messages)-1].Role == llm.ChatMessageRoleTool {
				return &llm.Result{Message: &llm.Message{
					Role:    llm.ChatMessageRoleAssistant,
					Content: messages[len(messages)-1].Content,
				}}, nil
			}

			return &llm.Result{Message: &llm.Message{
				Role: llm.ChatMessageRoleAssistant,
				ToolCalls: []openai.ToolCall{{
					ID:   "call_1",
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{
						Name:      toolNameCreateFile,
						Arguments: `null`,
					},
				}},
			}}, nil
		},
	}

	skillTool, err := (&Skill{
		Name:         "skill-tool",
		Description:  "test skill",
		Body:         "system instructions",
		RootPath:     t.TempDir(),
		AllowedTools: toolNameCreateFile,
	}).AsTool(client)
	if err != nil {
		t.Fatalf("AsTool() error = %v", err)
	}

	result, err := skillTool.Handle(context.Background(), `{"input":"create a file"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v", err)
	}

	if !strings.Contains(string(resultJSON), "tool arguments must be a JSON object") {
		t.Fatalf("result = %s, want parse error for null tool arguments", string(resultJSON))
	}
	if !strings.Contains(string(resultJSON), toolNameCreateFile) {
		t.Fatalf("result = %s, want tool name in tool execution error", string(resultJSON))
	}
}
