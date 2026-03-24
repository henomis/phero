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
		return nil, errors.New("nil input")
	}
	if strings.TrimSpace(input.Command) == "" {
		return nil, errors.New("command is required")
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
