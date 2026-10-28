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

	"github.com/henomis/phero/v2/llm"
)

// EditInput is the input schema for the edit tool.
type EditInput struct {
	FilePath   string `json:"file_path" jsonschema:"description=The absolute path to the file to modify"`
	OldString  string `json:"old_string" jsonschema:"description=The text to replace"`
	NewString  string `json:"new_string" jsonschema:"description=The text to replace it with (must be different from old_string)"`  //nolint:lll
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all occurrences of old_string (default false)"` //nolint:lll
}

// EditOutput is the output schema for the edit tool.
type EditOutput struct {
	Replacements int `json:"replacements"`
}

// EditTool performs precise string replacements in files.
type EditTool struct {
	tool        *llm.Tool
	workingDir  string
	maxFileSize int64
	tracker     *ReadTracker
	onWrite     WriteHook
}

// NewEditTool creates a new edit tool.
func NewEditTool(opts ...Option) (*EditTool, error) {
	o := applyOptions(opts...)

	e := &EditTool{workingDir: o.workingDir, maxFileSize: o.maxFileSize, tracker: o.tracker, onWrite: o.onWrite}

	tool, err := llm.NewTool(
		"edit",
		"Replace exact text in a file. By default old_string must appear exactly once.",
		e.edit,
	)
	if err != nil {
		return nil, err
	}

	// Edits of one round run alone and in order: each may depend on the last.
	e.tool = tool.SetExclusive(true)

	return e, nil
}

// Tool returns the underlying LLM tool.
func (e *EditTool) Tool() *llm.Tool {
	return e.tool
}

// Preview returns the change a call with input would make, without making
// it. It runs the call's code and checks, so it fails as the call would.
func (e *EditTool) Preview(input *EditInput) (*Change, error) {
	change, _, err := e.plan(input)

	return change, err
}

func (e *EditTool) edit(ctx context.Context, input *EditInput) (*EditOutput, error) {
	change, replacements, err := e.plan(input)
	if err != nil {
		return nil, err
	}

	if err = applyChange(ctx, change, e.tracker, e.onWrite); err != nil {
		return nil, err
	}

	return &EditOutput{Replacements: replacements}, nil
}

// plan returns the change a call with input makes, and the number of
// replacements.
func (e *EditTool) plan(input *EditInput) (*Change, int, error) {
	if input == nil {
		return nil, 0, fmt.Errorf("nil input")
	}

	if strings.TrimSpace(input.FilePath) == "" {
		return nil, 0, ErrPathRequired
	}

	return planEdits(e.workingDir, e.maxFileSize, e.tracker, input.FilePath, []EditOperation{{
		OldString: input.OldString, NewString: input.NewString, ReplaceAll: input.ReplaceAll,
	}})
}

// planEdits applies edits, in order, to the content of the file at path, and
// returns the change when every edit applies, with the number of
// replacements made. It does not write the file.
func planEdits(
	workingDir string, maxFileSize int64, tracker *ReadTracker, path string, edits []EditOperation,
) (*Change, int, error) {
	resolvedPath, err := resolveToolPath(workingDir, path)
	if err != nil {
		return nil, 0, err
	}

	if err = tracker.check(resolvedPath); err != nil {
		return nil, 0, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, 0, err
	}

	if maxFileSize > 0 && info.Size() > maxFileSize {
		return nil, 0, &TooLargeError{Path: resolvedPath, Size: info.Size(), Limit: maxFileSize}
	}

	before, err := os.ReadFile(resolvedPath) //nolint:gosec // path is resolved and validated above
	if err != nil {
		return nil, 0, err
	}

	content := before
	total := 0

	for i, edit := range edits {
		var n int

		if content, n, err = applyEdit(content, edit, resolvedPath); err != nil {
			if len(edits) > 1 {
				return nil, 0, fmt.Errorf("edit %d of %d: %w (no edit was applied)", i+1, len(edits), err)
			}

			return nil, 0, err
		}

		total += n
	}

	return &Change{Path: resolvedPath, Existed: true, Before: before, After: content, Perm: info.Mode().Perm()}, total, nil
}

// applyEdit replaces edit.OldString with edit.NewString in content: once, and
// then it must appear exactly once, or everywhere with ReplaceAll.
func applyEdit(content []byte, edit EditOperation, path string) ([]byte, int, error) {
	if edit.OldString == "" {
		return nil, 0, ErrOldStringRequired
	}

	if edit.OldString == edit.NewString {
		return nil, 0, fmt.Errorf("new_string must differ from old_string")
	}

	needle := []byte(edit.OldString)

	count := bytes.Count(content, needle)
	if count == 0 {
		return nil, 0, fmt.Errorf("old_string not found in %s", path)
	}

	if !edit.ReplaceAll && count != 1 {
		return nil, 0, fmt.Errorf("old_string found %d times in %s", count, path)
	}

	if !edit.ReplaceAll {
		count = 1
	}

	return bytes.Replace(content, needle, []byte(edit.NewString), count), count, nil
}
