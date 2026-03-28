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

package file

import (
	"context"
	"errors"
	"os"
	"strings"

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
	resolvedPath, err := resolveToolPath(w.workingDir, path)
	if err != nil {
		return nil, err
	}

	err = os.WriteFile(resolvedPath, []byte(input.Content), 0o644)
	if err != nil {
		return nil, err
	}
	return &CreateFileOutput{Len: len(input.Content)}, nil
}
