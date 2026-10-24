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
	"fmt"
	"strings"

	"github.com/henomis/phero/v2/llm"
)

// EditOperation is one replacement of a multi_edit call.
type EditOperation struct {
	OldString  string `json:"old_string" jsonschema:"description=The text to replace"`
	NewString  string `json:"new_string" jsonschema:"description=The text to replace it with (must be different from old_string)"`  //nolint:lll
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all occurrences of old_string (default false)"` //nolint:lll
}

// MultiEditInput is the input schema for the multi_edit tool.
type MultiEditInput struct {
	FilePath string          `json:"file_path" jsonschema:"description=The absolute path to the file to modify"`
	Edits    []EditOperation `json:"edits" jsonschema_description:"The edits to apply in order; each sees the result of the ones before it"` //nolint:lll
}

// MultiEditOutput is the output schema for the multi_edit tool.
type MultiEditOutput struct {
	Replacements int `json:"replacements"`
}

// MultiEditTool applies several exact replacements to one file at once.
type MultiEditTool struct {
	tool        *llm.Tool
	workingDir  string
	maxFileSize int64
	tracker     *ReadTracker
}

// NewMultiEditTool creates a new multi_edit tool.
func NewMultiEditTool(opts ...Option) (*MultiEditTool, error) {
	o := applyOptions(opts...)

	m := &MultiEditTool{workingDir: o.workingDir, maxFileSize: o.maxFileSize, tracker: o.tracker}

	tool, err := llm.NewTool(
		"multi_edit",
		"Apply several exact text replacements to one file, in order. Either every edit applies or none does. "+
			"Each old_string must appear exactly once unless replace_all is set.",
		m.multiEdit,
	)
	if err != nil {
		return nil, err
	}

	// Edits of one round run alone and in order: each may depend on the last.
	m.tool = tool.SetExclusive(true)

	return m, nil
}

// Tool returns the underlying LLM tool.
func (m *MultiEditTool) Tool() *llm.Tool {
	return m.tool
}

func (m *MultiEditTool) multiEdit(_ context.Context, input *MultiEditInput) (*MultiEditOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("nil input")
	}

	if strings.TrimSpace(input.FilePath) == "" {
		return nil, ErrPathRequired
	}

	if len(input.Edits) == 0 {
		return nil, ErrEditsRequired
	}

	replacements, err := editFile(m.workingDir, m.maxFileSize, m.tracker, input.FilePath, input.Edits)
	if err != nil {
		return nil, err
	}

	return &MultiEditOutput{Replacements: replacements}, nil
}
