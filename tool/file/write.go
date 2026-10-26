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

	"github.com/henomis/phero/v2/llm"
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
	tracker     *ReadTracker
	onWrite     WriteHook
}

// NewWriteTool creates a new write tool.
func NewWriteTool(opts ...Option) (*WriteTool, error) {
	o := applyOptions(opts...)

	w := &WriteTool{
		workingDir:  o.workingDir,
		noOverwrite: o.noOverwrite,
		tracker:     o.tracker,
		onWrite:     o.onWrite,
	}

	tool, err := llm.NewTool(
		"write",
		"Create or overwrite a file with the provided content.",
		w.write,
	)
	if err != nil {
		return nil, err
	}

	// Writes of one round run alone and in order: they may touch the same file.
	w.tool = tool.SetExclusive(true)

	return w, nil
}

// Tool returns the underlying LLM tool.
func (w *WriteTool) Tool() *llm.Tool {
	return w.tool
}

// Preview returns the change a call with input would make, without making
// it. It runs the call's code and checks, so it fails as the call would.
func (w *WriteTool) Preview(input *WriteInput) (*Change, error) {
	return w.plan(input, true)
}

func (w *WriteTool) write(ctx context.Context, input *WriteInput) (*WriteOutput, error) {
	// The old content is read only for the hook.
	change, err := w.plan(input, w.onWrite != nil)
	if err != nil {
		return nil, err
	}

	if err = applyChange(ctx, change, w.tracker, w.onWrite); err != nil {
		return nil, err
	}

	return &WriteOutput{BytesWritten: len(input.Content)}, nil
}

// plan returns the change a call with input makes. Its Before is read only
// when withBefore is true.
func (w *WriteTool) plan(input *WriteInput, withBefore bool) (*Change, error) {
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

	if err = w.tracker.check(resolvedPath); err != nil {
		return nil, err
	}

	change := &Change{Path: resolvedPath, After: []byte(input.Content), Perm: defaultFileMode}

	info, err := os.Stat(resolvedPath)
	if errors.Is(err, os.ErrNotExist) {
		return change, nil
	}

	if err != nil {
		return nil, err
	}

	if w.noOverwrite {
		return nil, ErrFileExists
	}

	change.Existed, change.Perm = true, info.Mode().Perm()

	if withBefore {
		if change.Before, err = os.ReadFile(resolvedPath); err != nil { //nolint:gosec // path is resolved and validated above
			return nil, err
		}
	}

	return change, nil
}

// WriteFileAtomic writes data to the file at path with permissions perm, as
// the write and edit tools do: to a temporary file in the same directory,
// then renamed over path, so a reader sees the old content or the new one,
// never a part. Use it to restore a Change's Before.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return atomicWriteFile(path, data, perm)
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
