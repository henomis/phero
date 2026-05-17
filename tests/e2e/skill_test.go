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

//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	skillpkg "github.com/henomis/phero/skill"
	toolfile "github.com/henomis/phero/tool/file"
	toolskill "github.com/henomis/phero/tool/skill"
)

const skillsRoot = "../../examples/skills/skills"

// TestSkillParser_ListAndParse verifies that skills can be discovered and parsed.
func TestSkillParser_ListAndParse(t *testing.T) {
	parser := skillpkg.New(skillsRoot)

	list, err := parser.List()
	if err != nil {
		t.Fatalf("Parser.List: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected at least one skill")
	}

	t.Logf("Discovered skills: %v", list)

	skillItem, err := parser.Parse(list[0])
	if err != nil {
		t.Fatalf("Parser.Parse: %v", err)
	}

	if strings.TrimSpace(skillItem.Name) == "" {
		t.Fatal("expected parsed skill to have a name")
	}
	if strings.TrimSpace(skillItem.Description) == "" {
		t.Fatal("expected parsed skill to have a description")
	}
}

// TestSkill_AsTool verifies that a skill dispatcher tool can be constructed and
// exposes a non-empty tool name.
func TestSkill_AsTool(t *testing.T) {
	st, err := toolskill.New(skillsRoot)
	if err != nil {
		t.Fatalf("toolskill.New: %v", err)
	}

	tool := st.Tool()
	if strings.TrimSpace(tool.Name()) == "" {
		t.Fatal("expected tool to have a name")
	}

	t.Logf("Skill tool name: %s", tool.Name())
}

// TestSkill_AgentIntegration verifies that a parsed skill tool can be added to
// an agent and used in a real run.
func TestSkill_AgentIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	st, err := toolskill.New(skillsRoot)
	if err != nil {
		t.Fatalf("toolskill.New: %v", err)
	}

	llmClient := buildOpenAILLM()

	a, err := agent.New(llmClient, "skill-agent", "Use your available tools when appropriate.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	if err := a.AddTool(st.Tool()); err != nil {
		t.Fatalf("AddTool skill: %v", err)
	}

	result, err := a.Run(ctx, llm.Text("Load the get-random-quote skill and tell me what it does."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	if strings.TrimSpace(result.TextContent()) == "" {
		t.Fatal("expected non-empty agent response")
	}

	t.Logf("Skill agent response: %q", result.TextContent())
}

// TestSkill_FileToolComposition verifies the pattern used in the example where
// a file tool is wrapped with custom middleware.
func TestSkill_FileToolComposition(t *testing.T) {
	ctx := context.Background()
	workDir := t.TempDir()

	writeTool, err := toolfile.NewWriteTool(toolfile.WithWorkingDirectory(workDir))
	if err != nil {
		t.Fatalf("NewWriteTool: %v", err)
	}

	wrapped := writeTool.Tool().Use(func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			var input *toolfile.WriteInput
			if err := json.Unmarshal([]byte(arguments), &input); err != nil {
				return nil, err
			}
			if input != nil && strings.TrimSpace(input.FilePath) == "" {
				input.FilePath = filepath.Join(workDir, "default.txt")
				patched, err := json.Marshal(input)
				if err != nil {
					return nil, err
				}
				arguments = string(patched)
			}
			return next(ctx, arguments)
		}
	})

	_, err = wrapped.Handle(ctx, `{"file_path":"","content":"hello from skill test"}`)
	if err != nil {
		t.Fatalf("wrapped.Handle: %v", err)
	}

	path := filepath.Join(workDir, "default.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%s): %v", path, err)
	}

	if string(data) != "hello from skill test" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}
