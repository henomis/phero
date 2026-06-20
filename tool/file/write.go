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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/henomis/phero/llm"
)

const defaultFileMode = 0o644

// WriteInput is the input schema for the write tool.
type WriteInput struct {
	FilePath string `json:"file_path" jsonschema:"description=The absolute path to the file to write"`
	Content  string `json:"content" jsonschema:"description=The content to write to the file"`
}

// WriteOutput is the output schema for the write tool.
type WriteOutput struct {
	BytesWritten int `json:"bytes_written"`
}

// WriteTool creates or overwrites files.
type WriteTool struct {
	tool        *llm.Tool
	workingDir  string
	noOverwrite bool
}

// NewWriteTool creates a new write tool.
func NewWriteTool(opts ...Option) (*WriteTool, error) {
	o := applyOptions(opts...)

	w := &WriteTool{
		workingDir:  o.workingDir,
		noOverwrite: o.noOverwrite,
	}

	tool, err := llm.NewTool(
		"write",
		"Create or overwrite a file with the provided content.",
		w.write,
	)
	if err != nil {
		return nil, err
	}

	w.tool = tool

	return w, nil
}

// Tool returns the underlying LLM tool.
func (w *WriteTool) Tool() *llm.Tool {
	return w.tool
}

func (w *WriteTool) write(_ context.Context, input *WriteInput) (*WriteOutput, error) {
	if input == nil {
		return nil, fmt.Errorf("nil input")
	}

	if strings.TrimSpace(input.FilePath) == "" {
		return nil, ErrPathRequired
	}

	resolvedPath, err := resolveToolPath(w.workingDir, input.FilePath)
	if err != nil {
		return nil, err
	}

	perm := os.FileMode(defaultFileMode)

	if info, statErr := os.Stat(resolvedPath); statErr == nil {
		if w.noOverwrite {
			return nil, ErrFileExists
		}

		perm = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, statErr
	}

	if mkdirErr := os.MkdirAll(filepath.Dir(resolvedPath), 0o750); mkdirErr != nil {
		return nil, mkdirErr
	}

	if writeErr := atomicWriteFile(resolvedPath, []byte(input.Content), perm); writeErr != nil {
		return nil, writeErr
	}

	return &WriteOutput{BytesWritten: len(input.Content)}, nil
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) (retErr error) {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".phero-write-*")
	if err != nil {
		return err
	}

	tmpPath := tmp.Name()

	defer func() {
		if retErr != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		return writeErr
	}

	if chmodErr := tmp.Chmod(perm); chmodErr != nil {
		_ = tmp.Close()
		return chmodErr
	}

	if closeErr := tmp.Close(); closeErr != nil {
		return closeErr
	}

	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		return renameErr
	}

	return nil
}
