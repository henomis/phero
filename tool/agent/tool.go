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

package agent

import (
	"context"
	"os"
	"strings"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/trace/text"
)

const (
	toolName        = "agent"
	toolDescription = "Create and run an agent at runtime with the given name, role description, and instructions. Use this tool to delegate a task to a purpose-built agent."
)

// Input defines the JSON input schema accepted by the tool.
type Input struct {
	Name        string `json:"name" jsonschema:"description=The name of the agent to create and run."`
	Description string `json:"description" jsonschema:"description=The system prompt or role description for the agent."`
	Input       string `json:"input" jsonschema:"description=Instructions for the delegated agent. Describe the task, question, or problem the agent should solve."`
}

// Output defines the JSON output schema returned by the tool.
type Output struct {
	Output string `json:"output" jsonschema:"description=The delegated agent response"`
}

// Tool wraps a delegated runner as an llm.Tool.
type Tool struct {
	tool      *llm.Tool
	llmClient llm.LLM
	tools     []*llm.Tool
}

// New creates a new agent tool.
//
// llmProvider must be non-nil. It is called at handler time with the agent name,
// description and input parts provided by the LLM.
func New(llmClient llm.LLM, tools ...*llm.Tool) (*Tool, error) {
	if llmClient == nil {
		return nil, ErrLLMRequired
	}

	t := &Tool{llmClient: llmClient, tools: tools}

	tool, err := llm.NewTool(
		toolName,
		toolDescription,
		t.handle,
	)
	if err != nil {
		return nil, err
	}

	t.tool = tool

	return t, nil
}

// Tool returns the underlying llm.Tool.
func (t *Tool) Tool() *llm.Tool {
	return t.tool
}

func (t *Tool) handle(ctx context.Context, input *Input) (*Output, error) {
	if input == nil {
		return nil, ErrNilInput
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, ErrNameRequired
	}

	description := strings.TrimSpace(input.Description)
	if description == "" {
		return nil, ErrDescriptionRequired
	}

	instructions := strings.TrimSpace(input.Input)
	if instructions == "" {
		return nil, ErrInputRequired
	}

	subAgent, err := agent.New(t.llmClient, name, description)
	if err != nil {
		return nil, err
	}

	for _, tool := range t.tools {
		if addErr := subAgent.AddTool(tool); addErr != nil {
			return nil, addErr
		}
	}

	subAgent.SetTracer(text.New(os.Stdout))

	result, err := subAgent.Run(ctx, llm.Text(instructions))
	if err != nil {
		return nil, err
	}

	return &Output{Output: result.TextContent()}, nil
}
