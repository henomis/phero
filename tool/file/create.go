package file

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/henomis/phero/llm"
)

// toolOptions holds shared configuration for file tools in this package.
type toolOptions struct {
	workingDir string
}

// Option is a configuration function for file tools.
type Option func(*toolOptions)

// WithWorkingDirectory sets the working directory used to resolve relative paths.
// When an input path is not absolute it is joined with this directory.
func WithWorkingDirectory(dir string) Option {
	return func(o *toolOptions) {
		o.workingDir = dir
	}
}

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
	workingDir string
}

// NewCreateFileTool creates a new instance of CreateFileTool.
func NewCreateFileTool(opts ...Option) (*CreateFileTool, error) {
	name := "create_file"
	description := "use this tool to create or overwrite a file with the specified content."

	o := &toolOptions{}
	for _, opt := range opts {
		opt(o)
	}

	createFileTool := &CreateFileTool{workingDir: o.workingDir}

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

// Tool returns the llm.FunctionTool representation of the CreateFileTool.
func (w *CreateFileTool) Tool() *llm.Tool {
	return w.tool
}

func (w *CreateFileTool) write(ctx context.Context, input *CreateFileInput) (*CreateFileOutput, error) {
	_ = ctx
	if input == nil {
		return nil, errors.New("nil input")
	}
	if strings.TrimSpace(input.Path) == "" {
		return nil, errors.New("path is required")
	}

	path := input.Path
	if w.workingDir != "" && !filepath.IsAbs(path) {
		path = filepath.Join(w.workingDir, path)
	}
	if w.workingDir != "" {
		rel, relErr := filepath.Rel(filepath.Clean(w.workingDir), filepath.Clean(path))
		if relErr != nil || strings.HasPrefix(rel, "..") {
			return nil, errors.New("path is outside the working directory")
		}
	}

	err := os.WriteFile(path, []byte(input.Content), 0o644)
	if err != nil {
		return nil, err
	}
	return &CreateFileOutput{Len: len(input.Content)}, nil
}
