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
	"os"
	"strings"
	"unicode/utf8"

	"github.com/henomis/phero/llm"
)

const (
	defaultReadLimit = 2000
	maxLineChars     = 2000
)

// ReadInput is the input schema for the read tool.
type ReadInput struct {
	FilePath string `json:"file_path" jsonschema:"description=The absolute path to the file to read"`
	Offset   int    `json:"offset,omitempty" jsonschema:"description=The line number to start reading from. Only provide if the file is too large to read at once"` //nolint:lll
	Limit    int    `json:"limit,omitempty" jsonschema:"description=The number of lines to read. Only provide if the file is too large to read at once"` //nolint:lll
}

// ReadOutput is the output schema for the read tool.
type ReadOutput struct {
	Content string `json:"content"`
}

// ReadTool reads files with cat -n style line numbering.
type ReadTool struct {
	tool        *llm.Tool
	workingDir  string
	maxFileSize int64
}

// NewReadTool creates a new read tool.
func NewReadTool(opts ...Option) (*ReadTool, error) {
	o := applyOptions(opts...)

	r := &ReadTool{
		workingDir:  o.workingDir,
		maxFileSize: o.maxFileSize,
	}

	tool, err := llm.NewTool(
		"read",
		"Read file contents from an absolute path with optional line offset/limit.",
		r.read,
	)
	if err != nil {
		return nil, err
	}

	r.tool = tool

	return r, nil
}

// Tool returns the underlying LLM tool.
func (r *ReadTool) Tool() *llm.Tool {
	return r.tool
}

func (r *ReadTool) read(_ context.Context, input *ReadInput) (*ReadOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("nil input")
	}

	if strings.TrimSpace(input.FilePath) == "" {
		return nil, ErrPathRequired
	}

	resolvedPath, err := resolveToolPath(r.workingDir, input.FilePath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", resolvedPath)
	}

	if r.maxFileSize > 0 && info.Size() > r.maxFileSize {
		return nil, &TooLargeError{Path: resolvedPath, Size: info.Size(), Limit: r.maxFileSize}
	}

	contentBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return nil, err
	}

	content := bytesToUTF8WithHexEscapes(contentBytes)
	lines := strings.Split(content, "\n")

	offset := input.Offset
	if offset < 0 {
		offset = 0
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultReadLimit
	}

	if offset >= len(lines) {
		return &ReadOutput{Content: ""}, nil
	}

	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}

	var b strings.Builder

	for i := offset; i < end; i++ {
		line := truncateRunes(lines[i], maxLineChars)
		fmt.Fprintf(&b, "%6d\t%s\n", i+1, line)
	}

	return &ReadOutput{Content: strings.TrimRight(b.String(), "\n")}, nil
}

func bytesToUTF8WithHexEscapes(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}

	var out strings.Builder
	out.Grow(len(b))

	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&out, "\\x%02x", b[0])
			b = b[1:]

			continue
		}

		out.WriteRune(r)

		b = b[size:]
	}

	return out.String()
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}

	r := []rune(s)
	if len(r) <= limit {
		return s
	}

	return string(r[:limit])
}
