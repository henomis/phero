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

// ErrFileTooLarge is returned when a file exceeds the configured maximum size
// for text viewing.
var ErrFileTooLarge = errors.New("file too large")

// ErrImageTooLarge is returned when an image file exceeds the configured
// maximum size for base64 rendering.
var ErrImageTooLarge = errors.New("image too large")

// FileTooLargeError carries the file path, actual size, and configured limit.
type FileTooLargeError struct {
	Path  string
	Size  int64
	Limit int64
}

func (e *FileTooLargeError) Error() string {
	return fmt.Sprintf("%s: file size %d bytes exceeds limit of %d bytes", e.Path, e.Size, e.Limit)
}

func (e *FileTooLargeError) Is(target error) bool {
	return target == ErrFileTooLarge
}

// ImageTooLargeError carries the image path, actual size, and configured limit.
type ImageTooLargeError struct {
	Path  string
	Size  int64
	Limit int64
}

func (e *ImageTooLargeError) Error() string {
	return fmt.Sprintf("%s: image size %d bytes exceeds limit of %d bytes", e.Path, e.Size, e.Limit)
}

func (e *ImageTooLargeError) Is(target error) bool {
	return target == ErrImageTooLarge
}
