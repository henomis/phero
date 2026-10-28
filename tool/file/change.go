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
	"os"
	"path/filepath"
)

// Change is what a write, edit or multi_edit call does to a file: the
// content before and after it. Preview returns one without writing it, and a
// WriteHook gets one after the tool wrote it.
type Change struct {
	// Path is the file's resolved path (see ResolvePath).
	Path string
	// Existed reports whether the file existed before the call.
	Existed bool
	// Before is the file's content before the call; nil when it did not
	// exist.
	Before []byte
	// After is the file's content after the call.
	After []byte
	// Perm is the file's permissions: kept for an existing file.
	Perm os.FileMode
}

// WriteHook is called by the write, edit and multi_edit tools after they
// wrote a change (WithOnWrite), in the tool call's context
// (llm.ToolCallFromContext gives the call). It is not called when the call
// fails. The tools are exclusive, so within one agent run the calls do not
// overlap; a hook shared by several agents must be safe for concurrent use.
// It must not change the Change's content.
type WriteHook func(ctx context.Context, change Change)

// applyChange writes change to disk, records it as seen by the model, and
// calls hook.
func applyChange(ctx context.Context, change *Change, tracker *ReadTracker, hook WriteHook) error {
	if !change.Existed {
		if err := os.MkdirAll(filepath.Dir(change.Path), 0o750); err != nil {
			return err
		}
	}

	if err := atomicWriteFile(change.Path, change.After, change.Perm); err != nil {
		return err
	}

	// The model knows the content it wrote: it may change it again.
	tracker.record(change.Path)

	if hook != nil {
		hook(ctx, *change)
	}

	return nil
}
