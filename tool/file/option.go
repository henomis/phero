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

import "sync"

type toolOptions struct {
	workingDir  string
	maxFileSize int64
	noOverwrite bool
	session     *Session
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

// WithSession sets a shared read-tracking session across fs tools.
func WithSession(session *Session) Option {
	return func(o *toolOptions) {
		o.session = session
	}
}

// Session tracks which files have been read in the current tool session.
type Session struct {
	mu    sync.RWMutex
	paths map[string]struct{}
}

// NewSession creates a new empty read-tracking session.
func NewSession() *Session {
	return &Session{paths: make(map[string]struct{})}
}

// MarkRead marks a resolved absolute path as read.
func (s *Session) MarkRead(path string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paths[path] = struct{}{}
}

// HasRead reports whether a resolved absolute path has been read.
func (s *Session) HasRead(path string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.paths[path]
	return ok
}

func applyOptions(opts ...Option) *toolOptions {
	o := &toolOptions{}
	for _, opt := range opts {
		opt(o)
	}
	if o.session == nil {
		o.session = NewSession()
	}
	return o
}
