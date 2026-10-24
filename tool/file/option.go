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

type toolOptions struct {
	workingDir  string
	maxFileSize int64
	noOverwrite bool
	maxResults  int
	noIgnore    bool
}

// Option configures fs tools.
type Option func(*toolOptions)

// WithWorkingDirectory sets the working directory used for path confinement.
func WithWorkingDirectory(dir string) Option {
	return func(o *toolOptions) {
		o.workingDir = dir
	}
}

// WithMaxFileSize sets the maximum file size in bytes for read operations.
// A value of 0 disables the limit.
func WithMaxFileSize(bytes int64) Option {
	return func(o *toolOptions) {
		o.maxFileSize = bytes
	}
}

// WithNoOverwrite configures write to fail when the target file already exists.
func WithNoOverwrite() Option {
	return func(o *toolOptions) {
		o.noOverwrite = true
	}
}

// WithMaxResults sets how many paths glob, and entries grep, return at most;
// a result cut short says so with Truncated. The default is
// DefaultMaxResults; a value of 0 or less removes the cap.
func WithMaxResults(n int) Option {
	return func(o *toolOptions) {
		o.maxResults = n
	}
}

// WithNoIgnore makes glob and grep search everything under their root:
// .gitignore files and the node_modules, .hg and .svn directories are no
// longer skipped. .git is always skipped.
func WithNoIgnore() Option {
	return func(o *toolOptions) {
		o.noIgnore = true
	}
}

func applyOptions(opts ...Option) *toolOptions {
	o := &toolOptions{maxResults: DefaultMaxResults}
	for _, opt := range opts {
		opt(o)
	}

	return o
}
