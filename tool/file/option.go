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

import "strings"

type toolOptions struct {
	workingDir  string
	maxFileSize int64
	maxOutput   int
	noOverwrite bool
	maxResults  int
	noIgnore    bool
	tracker     *ReadTracker
	onWrite     WriteHook
	readRoots   []string
}

// DefaultMaxFileSize is the read tool's size limit for a whole-file read
// (no offset or limit) when WithMaxFileSize is not given: 10 MiB. It guards
// memory; a page of a larger file can still be read with offset and limit.
const DefaultMaxFileSize = 10 << 20

// DefaultMaxOutputChars is how much text the read, grep and glob tools return
// at most when WithMaxOutputChars is not given, as the bash tool's output cap:
// about 7,500 tokens, so one call cannot fill the model's context.
const DefaultMaxOutputChars = 30_000

// Option configures fs tools.
type Option func(*toolOptions)

// WithWorkingDirectory sets the working directory used for path confinement.
func WithWorkingDirectory(dir string) Option {
	return func(o *toolOptions) {
		o.workingDir = dir
	}
}

// WithReadOnlyRoots lets the read, grep and glob tools also reach the files
// under dirs, outside the working directory, such as the directories of the
// user's skills, whose SKILL.md points to scripts and references. Paths are
// checked after their symlinks are resolved, as for the working directory.
// The write, edit and multi_edit tools ignore it: they stay confined to the
// working directory. Without a working directory, every path is reachable and
// it has no effect. Each call adds to the roots given before.
func WithReadOnlyRoots(dirs ...string) Option {
	return func(o *toolOptions) {
		for _, dir := range dirs {
			if strings.TrimSpace(dir) != "" {
				o.readRoots = append(o.readRoots, dir)
			}
		}
	}
}

// WithMaxFileSize sets the maximum file size in bytes for a whole-file read,
// one without offset or limit. The default is DefaultMaxFileSize; a value of 0
// disables the limit.
func WithMaxFileSize(bytes int64) Option {
	return func(o *toolOptions) {
		o.maxFileSize = bytes
	}
}

// WithMaxOutputChars sets how many characters the read, grep and glob tools
// return at most. A read cut short ends with a note giving the offset to read
// on from; grep and glob drop the entries that do not fit and set Truncated.
// The default is DefaultMaxOutputChars; a value of 0 or less removes the cap.
func WithMaxOutputChars(n int) Option {
	return func(o *toolOptions) {
		o.maxOutput = n
	}
}

// WithNoOverwrite configures write to fail when the target file already exists.
func WithNoOverwrite() Option {
	return func(o *toolOptions) {
		o.noOverwrite = true
	}
}

// WithMaxResults sets how many paths glob, and entries grep, return at most;
// a result cut short says so with Truncated. Grep's head_limit can only lower
// it. The default is
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

// WithReadTracker shares tracker between the read, write, edit and
// multi_edit tools: read records the files it reads, and the others refuse to
// change an existing file that was not read, or that changed since. Give the
// same tracker to every tool of an agent.
func WithReadTracker(tracker *ReadTracker) Option {
	return func(o *toolOptions) {
		o.tracker = tracker
	}
}

// WithOnWrite makes the write, edit and multi_edit tools call hook after each
// change they write, with the content they read and wrote, for example to
// record it for an undo. See WriteHook.
func WithOnWrite(hook WriteHook) Option {
	return func(o *toolOptions) {
		o.onWrite = hook
	}
}

func applyOptions(opts ...Option) *toolOptions {
	o := &toolOptions{maxResults: DefaultMaxResults, maxFileSize: DefaultMaxFileSize, maxOutput: DefaultMaxOutputChars}
	for _, opt := range opts {
		opt(o)
	}

	return o
}
