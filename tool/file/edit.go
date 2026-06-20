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
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/henomis/phero/llm"
)

// EditInput is the input schema for the edit tool.
type EditInput struct {
	FilePath   string `json:"file_path" jsonschema:"description=The absolute path to the file to modify"`
	OldString  string `json:"old_string" jsonschema:"description=The text to replace"`
	NewString  string `json:"new_string" jsonschema:"description=The text to replace it with (must be different from old_string)"` //nolint:lll
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all occurrences of old_string (default false)"` //nolint:lll
}

// EditOutput is the output schema for the edit tool.
type EditOutput struct {
	Replacements int `json:"replacements"`
}

// EditTool performs precise string replacements in files.
type EditTool struct {
	tool       *llm.Tool
	workingDir string
}

// NewEditTool creates a new edit tool.
func NewEditTool(opts ...Option) (*EditTool, error) {
	o := applyOptions(opts...)

	e := &EditTool{workingDir: o.workingDir}

	tool, err := llm.NewTool(
		"edit",
		"Replace exact text in a file. By default old_string must appear exactly once.",
		e.edit,
	)
	if err != nil {
		return nil, err
	}

	e.tool = tool

	return e, nil
}

// Tool returns the underlying LLM tool.
func (e *EditTool) Tool() *llm.Tool {
	return e.tool
}

func (e *EditTool) edit(_ context.Context, input *EditInput) (*EditOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("nil input")
	}

	if strings.TrimSpace(input.FilePath) == "" {
		return nil, ErrPathRequired
	}

	if input.OldString == "" {
		return nil, ErrOldStringRequired
	}

	if input.OldString == input.NewString {
		return nil, fmt.Errorf("new_string must differ from old_string")
	}

	resolvedPath, err := resolveToolPath(e.workingDir, input.FilePath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, err
	}

	mode := info.Mode().Perm()

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, err
	}

	needle := []byte(input.OldString)
	replacement := []byte(input.NewString)

	count := bytes.Count(content, needle)
	if count == 0 {
		return nil, fmt.Errorf("old_string not found in %s", resolvedPath)
	}

	if !input.ReplaceAll && count != 1 {
		return nil, fmt.Errorf("old_string found %d times in %s", count, resolvedPath)
	}

	maxReplacements := count
	if !input.ReplaceAll {
		maxReplacements = 1
	}

	updated := bytes.Replace(content, needle, replacement, maxReplacements)
	if writeErr := atomicWriteFile(resolvedPath, updated, mode); writeErr != nil {
		return nil, writeErr
	}

	if !input.ReplaceAll {
		return &EditOutput{Replacements: 1}, nil
	}

	return &EditOutput{Replacements: count}, nil
}
