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

package bash

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/henomis/phero/llm"
)

// Input represents the input for the bash_tool.
//
// Command is the bash command to execute. Description provides context on why the command is being run.
type Input struct {
	Command     string `json:"command" jsonschema:"description=bash command to run"`
	Description string `json:"description" jsonschema:"description=why you're running it"`
}

// Output represents the output from bash_tool.
//
// It returns stdout + stderr combined as a plain text string.
type Output struct {
	Output string `json:"output"`
}

// Tool is a tool that runs bash commands.
type Tool struct {
	tool       *llm.Tool
	workingDir string
}

// Option represents a configuration option for the bash_tool.
type Option func(*Tool)

// New creates a new instance of the bash_tool.
func New(options ...Option) (*Tool, error) {
	name := "bash"
	description := "Use this tool to run bash commands"

	bashTool := &Tool{}

	for _, option := range options {
		option(bashTool)
	}

	tool, err := llm.NewTool(
		name,
		description,
		bashTool.run,
	)
	if err != nil {
		return nil, err
	}

	bashTool.tool = tool
	return bashTool, nil
}

// Tool returns the llm.Tool representation.
func (t *Tool) Tool() *llm.Tool {
	return t.tool
}

func WithWorkingDirectory(dir string) Option {
	return func(t *Tool) {
		t.workingDir = dir
	}
}

func (t *Tool) run(ctx context.Context, input *Input) (*Output, error) {
	if input == nil {
		return nil, ErrNilInput
	}
	if strings.TrimSpace(input.Command) == "" {
		return nil, ErrCommandRequired
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", input.Command)
	if t.workingDir != "" {
		cmd.Dir = t.workingDir
	}

	combined, err := cmd.CombinedOutput()
	out := string(combined)

	if err == nil {
		return &Output{Output: out}, nil
	}

	// If the command executed but failed (non-zero exit), return output with an inline marker
	// so callers can infer failure without an explicit exit code field.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += fmt.Sprintf("exit code: %d", exitErr.ExitCode())
		return &Output{Output: out}, nil
	}

	// For execution failures (e.g., bash missing), surface the error.
	return nil, err
}
