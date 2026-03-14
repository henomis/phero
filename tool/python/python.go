package python

import (
	"bytes"
	"context"
	"os/exec"

	"github.com/henomis/phero/llm"
)

// Input represents the input for the GoTool.
type Input struct {
	Args []string `json:"args"`
}

// Output represents the output from running the GoTool.
type Output struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Error  string `json:"error,omitempty"`
}

// Tool is a tool that allows running Python commands.
type Tool struct {
	workingDirectory string
	tool             *llm.Tool
	validateFn       func(context.Context, *Input) error
}

// New creates a new instance of Tool.
//
// If skipPermission is true, the tool will run without asking for user confirmation.
// Otherwise, it will prompt the user for permission before executing the command.
// path specifies the working directory for the Python command.
func New() (*Tool, error) {
	name := "python"
	description := "use this tool to run python command."

	pythonTool := &Tool{
		workingDirectory: ".",
	}

	tool, err := llm.NewTool(
		name,
		description,
		pythonTool.run,
	)
	if err != nil {
		return nil, err
	}

	pythonTool.tool = tool

	return pythonTool, nil
}

// WithValidation allows setting a custom validation function for the PythonTool.
func (g *Tool) WithValidation(validateFn func(context.Context, *Input) error) *Tool {
	g.validateFn = validateFn
	return g
}

// WithWorkingDirectory sets the working directory for the PythonTool.
func (g *Tool) WithWorkingDirectory(workingDirectory string) *Tool {
	g.workingDirectory = workingDirectory
	return g
}

// Tool returns the llm.FunctionTool representation of the PythonTool.
func (g *Tool) Tool() *llm.Tool {
	return g.tool
}

func (g *Tool) run(ctx context.Context, input *Input) (*Output, error) {
	if g.validateFn != nil {
		if err := g.validateFn(ctx, input); err != nil {
			return nil, err
		}
	}

	cmd := exec.CommandContext(ctx, "python", input.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = g.workingDirectory

	err := cmd.Run()
	output := &Output{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if err != nil {
		output.Error = err.Error()
	}
	return output, nil
}
