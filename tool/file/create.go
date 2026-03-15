package file

import (
	"context"
	"os"

	"github.com/henomis/phero/llm"
)

// CreateFileInput represents the input for the CreateFileTool, containing the path of the file to create and the content to write.
type CreateFileInput struct {
	Description string `json:"description" jsonschema:"description=Why you're creating this file."`
	Path        string `json:"path" jsonschema:"description=The path of the file to create."`
	Content     string `json:"content" jsonschema:"description=The content to write to the file."`
}

// CreateFileOutput represents the output from running the CreateFileTool, containing the length of the content written.
type CreateFileOutput struct {
	Len int `json:"len" jsonschema:"description=The length of the content written to the file."`
}

// CreateFileTool is a tool that allows writing content to a file.
type CreateFileTool struct {
	tool       *llm.Tool
	validateFn func(context.Context, *CreateFileInput) error
}

// NewCreateFileTool creates a new instance of CreateFileTool.
//
// If skipPermission is true, the tool will run without asking for user confirmation.
// Otherwise, it will prompt the user for permission before executing the command.
// path specifies the base directory for writing files.
func NewCreateFileTool() (*CreateFileTool, error) {
	name := "create_file"
	description := "use this tool to create a file with specified content."

	createFileTool := &CreateFileTool{}

	tool, err := llm.NewTool(
		name,
		description,
		createFileTool.write,
	)
	if err != nil {
		return nil, err
	}

	createFileTool.tool = tool

	return createFileTool, nil
}

// WithValidation allows setting a custom validation function for the CreateFileTool.
func (w *CreateFileTool) WithValidation(validateFn func(context.Context, *CreateFileInput) error) *CreateFileTool {
	w.validateFn = validateFn
	return w
}

// Tool returns the llm.FunctionTool representation of the CreateFileTool.
func (w *CreateFileTool) Tool() *llm.Tool {
	return w.tool
}

func (w *CreateFileTool) write(ctx context.Context, input *CreateFileInput) (*CreateFileOutput, error) {
	if w.validateFn != nil {
		if err := w.validateFn(ctx, input); err != nil {
			return nil, err
		}
	}

	err := os.WriteFile(input.Path, []byte(input.Content), 0o644)
	if err != nil {
		return nil, err
	}
	return &CreateFileOutput{Len: len(input.Content)}, nil
}
