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

// toolOptions holds shared configuration for file tools in this package.
type toolOptions struct {
	workingDir  string
	maxFileSize int64 // 0 means no limit; applies to both text and image reads in ViewTool
	noOverwrite bool  // if true, CreateFileTool refuses to overwrite existing files
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

// WithMaxFileSize sets the maximum number of bytes that may be read from any
// file (text or image) during a view operation.
// Text files exceeding the limit return ErrFileTooLarge; images return ErrImageTooLarge.
//
// A value of 0 disables the limit (default).
func WithMaxFileSize(bytes int64) Option {
	return func(o *toolOptions) {
		o.maxFileSize = bytes
	}
}

// WithNoOverwrite configures CreateFileTool to return ErrFileExists when the
// target file already exists, instead of silently overwriting it.
func WithNoOverwrite() Option {
	return func(o *toolOptions) {
		o.noOverwrite = true
	}
}
