package file

import (
	"context"
	"os"

	"github.com/henomis/phero/llm"
)

// WriteInput represents the input for the WriteTool, containing the path of the file to write and the content to write.
type WriteInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteOutput represents the output from running the WriteTool, containing the length of the content written.
type WriteOutput struct {
	Len int `json:"len"`
}

// WriteTool is a tool that allows writing content to a file.
type WriteTool struct {
	tool       *llm.Tool
	validateFn func(context.Context, *WriteInput) error
}

// NewWriteTool creates a new instance of WriteTool.
//
// If skipPermission is true, the tool will run without asking for user confirmation.
// Otherwise, it will prompt the user for permission before executing the command.
// path specifies the base directory for writing files.
func NewWriteTool() (*WriteTool, error) {
	name := "write_file"
	description := "use this tool to write content to a file. The input is the file path and the content to write. The output is the length of the content written."

	writeTool := &WriteTool{}

	tool, err := llm.NewTool(
		name,
		description,
		writeTool.write,
	)
	if err != nil {
		return nil, err
	}

	writeTool.tool = tool

	return writeTool, nil
}

// WithValidation allows setting a custom validation function for the WriteTool.
func (w *WriteTool) WithValidation(validateFn func(context.Context, *WriteInput) error) *WriteTool {
	w.validateFn = validateFn
	return w
}

// Tool returns the llm.FunctionTool representation of the WriteTool.
func (w *WriteTool) Tool() *llm.Tool {
	return w.tool
}

func (w *WriteTool) write(ctx context.Context, input *WriteInput) (*WriteOutput, error) {
	if w.validateFn != nil {
		if err := w.validateFn(ctx, input); err != nil {
			return nil, err
		}
	}

	err := os.WriteFile(input.Path, []byte(input.Content), 0o644)
	if err != nil {
		return nil, err
	}
	return &WriteOutput{Len: len(input.Content)}, nil
}
