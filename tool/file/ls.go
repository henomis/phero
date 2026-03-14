package file

import (
	"context"
	"os"

	"github.com/henomis/phero/llm"
)

// ListInput represents the input for the ListTool, containing the path of the directory to list.
type ListInput struct {
	Path string `json:"path"`
}

// Info represents a file or directory with its name and whether it is a directory.
type Info struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

// ListOutput represents the output of the ListTool, containing a list of files.
type ListOutput struct {
	Files []Info `json:"files"`
}

// ListTool is a tool that allows listing files in a directory.
type ListTool struct {
	tool       *llm.Tool
	validateFn func(context.Context, *ListInput) error
}

// NewListTool creates a new instance of ListTool.
//
// If skipPermission is true, the tool will run without asking for user confirmation.
// Otherwise, it will prompt the user for permission before executing the command.
// path specifies the base directory for listing files.
func NewListTool() (*ListTool, error) {
	name := "list_files"
	description := "use this tool to list files in a directory. The input is the directory path. The output is a list of files in the directory, with their name and whether they are directories."

	listTool := &ListTool{}

	tool, err := llm.NewTool(
		name,
		description,
		listTool.list,
	)
	if err != nil {
		return nil, err
	}

	listTool.tool = tool

	return listTool, nil
}

// WithValidation allows setting a custom validation function for the ListTool.
func (l *ListTool) WithValidation(validateFn func(context.Context, *ListInput) error) *ListTool {
	l.validateFn = validateFn
	return l
}

// Tool returns the llm.FunctionTool representation of the ListTool.
func (l *ListTool) Tool() *llm.Tool {
	return l.tool
}

func (l *ListTool) list(ctx context.Context, input *ListInput) (*ListOutput, error) {
	if l.validateFn != nil {
		if err := l.validateFn(ctx, input); err != nil {
			return nil, err
		}
	}

	entries, err := os.ReadDir(input.Path)
	if err != nil {
		return nil, err
	}

	files := []Info{}
	for _, entry := range entries {
		files = append(files, Info{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
		})
	}

	return &ListOutput{Files: files}, nil
}
