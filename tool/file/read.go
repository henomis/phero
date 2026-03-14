package file

import (
	"context"
	"os"

	"github.com/henomis/phero/llm"
)

// ReadInput represents the input for the ReadTool, containing the path of the file to read.
type ReadInput struct {
	Path string `json:"path"`
}

// ReadOutput represents the output from running the ReadTool, containing the content of the file.
type ReadOutput struct {
	Content string `json:"content"`
}

// ReadTool is a tool that allows reading the content of a file.
type ReadTool struct {
	tool       *llm.Tool
	validateFn func(context.Context, *ReadInput) error
}

// NewReadTool creates a new instance of ReadTool.
//
// If skipPermission is true, the tool will run without asking for user confirmation.
// Otherwise, it will prompt the user for permission before executing the command.
// path specifies the base directory for reading files.
func NewReadTool() (*ReadTool, error) {
	name := "read_file"
	description := "use this tool to read the content of a file. The input is the file path. The output is the content of the file."

	readTool := &ReadTool{}

	tool, err := llm.NewTool(
		name,
		description,
		readTool.read,
	)
	if err != nil {
		return nil, err
	}

	readTool.tool = tool

	return readTool, nil
}

// WithValidation allows setting a custom validation function for the ReadTool.
func (r *ReadTool) WithValidation(validateFn func(context.Context, *ReadInput) error) *ReadTool {
	r.validateFn = validateFn
	return r
}

// Tool returns the llm.FunctionTool representation of the ReadTool.
func (r *ReadTool) Tool() *llm.Tool {
	return r.tool
}

func (r *ReadTool) read(ctx context.Context, input *ReadInput) (*ReadOutput, error) {
	if r.validateFn != nil {
		if err := r.validateFn(ctx, input); err != nil {
			return nil, err
		}
	}

	content, err := os.ReadFile(input.Path)
	if err != nil {
		return nil, err
	}
	return &ReadOutput{Content: string(content)}, nil
}
