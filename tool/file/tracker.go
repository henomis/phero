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
	"os"
	"sync"
	"time"
)

// ReadTracker remembers which files the read tool has read, so the write and
// edit tools can refuse to change a file the model has not seen, or that
// changed on disk since it read it. Share one tracker between the tools of an
// agent with WithReadTracker.
type ReadTracker struct {
	mu    sync.Mutex
	files map[string]fileStamp
}

// fileStamp identifies the version of a file the model has seen.
type fileStamp struct {
	modTime time.Time
	size    int64
}

// NewReadTracker returns an empty ReadTracker.
func NewReadTracker() *ReadTracker {
	return &ReadTracker{files: map[string]fileStamp{}}
}

// record notes the current version of path as seen. A nil tracker does
// nothing.
func (t *ReadTracker) record(path string) {
	if t == nil {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.files[path] = fileStamp{modTime: info.ModTime(), size: info.Size()}
}

// check returns ErrFileNotRead when path exists and was never read, and a
// *ChangedError when it changed since. A file that does not exist yet
// needs no read. A nil tracker allows everything.
func (t *ReadTracker) check(path string) error {
	if t == nil {
		return nil
	}

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	seen, ok := t.files[path]
	if !ok {
		return fmt.Errorf("%w: %s", ErrFileNotRead, path)
	}

	if !seen.modTime.Equal(info.ModTime()) || seen.size != info.Size() {
		return &ChangedError{Path: path}
	}

	return nil
}
