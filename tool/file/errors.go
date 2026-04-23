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
	"errors"
	"fmt"
)

// ErrPathRequired is returned when a required file path is missing.
var ErrPathRequired = errors.New("file_path is required")

// ErrPatternRequired is returned when a required pattern is missing.
var ErrPatternRequired = errors.New("pattern is required")

// ErrPathMustBeAbsolute is returned when a path argument is not absolute.
var ErrPathMustBeAbsolute = errors.New("path must be absolute")

// ErrPathOutsideWorkingDirectory is returned when a resolved path escapes the configured working directory.
var ErrPathOutsideWorkingDirectory = errors.New("path is outside the working directory")

// ErrFileTooLarge is returned when a file exceeds the configured read size limit.
var ErrFileTooLarge = errors.New("file too large")

// ErrReadRequired is returned when a write/edit operation is attempted without a prior read in the session.
var ErrReadRequired = errors.New("read is required before write/edit")

// ErrFileExists is returned when write is configured to disallow overwriting existing files.
var ErrFileExists = errors.New("file already exists")

// ErrOldStringRequired is returned when edit is called without old_string.
var ErrOldStringRequired = errors.New("old_string is required")

// ErrInvalidOutputMode is returned when grep receives an unsupported output_mode value.
var ErrInvalidOutputMode = errors.New("invalid output_mode")

// ErrContextFlagsRequireContentMode is returned when -A/-B/-C/-n are used with a non-content mode.
var ErrContextFlagsRequireContentMode = errors.New("-A/-B/-C/-n require output_mode=content")

// FileTooLargeError includes the path, actual file size, and configured limit.
type FileTooLargeError struct {
	Path  string
	Size  int64
	Limit int64
}

// Error returns the formatted file-too-large message.
func (e *FileTooLargeError) Error() string {
	return fmt.Sprintf("%s: file size %d bytes exceeds limit of %d bytes", e.Path, e.Size, e.Limit)
}

// Is reports sentinel compatibility for errors.Is.
func (e *FileTooLargeError) Is(target error) bool {
	return target == ErrFileTooLarge
}

// ReadRequiredError includes the target path requiring prior read.
type ReadRequiredError struct {
	Path string
}

// Error returns the formatted read-required message.
func (e *ReadRequiredError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, ErrReadRequired)
}

// Is reports sentinel compatibility for errors.Is.
func (e *ReadRequiredError) Is(target error) bool {
	return target == ErrReadRequired
}
