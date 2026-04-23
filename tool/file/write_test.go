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
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTool_RequiresReadForExistingFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "existing.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	session := NewSession()
	tool, err := NewWriteTool(WithWorkingDirectory(tmp), WithSession(session))
	if err != nil {
		t.Fatalf("new write tool: %v", err)
	}

	_, err = tool.write(context.Background(), &WriteInput{FilePath: path, Content: "new"})
	if !errors.Is(err, ErrReadRequired) {
		t.Fatalf("expected ErrReadRequired, got %v", err)
	}

	session.MarkRead(path)
	_, err = tool.write(context.Background(), &WriteInput{FilePath: path, Content: "new"})
	if err != nil {
		t.Fatalf("write with prior read failed: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(b) != "new" {
		t.Fatalf("unexpected file content: %q", string(b))
	}
}

func TestWriteTool_NewFileDoesNotRequireRead(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "new.txt")

	tool, err := NewWriteTool(WithWorkingDirectory(tmp), WithSession(NewSession()))
	if err != nil {
		t.Fatalf("new write tool: %v", err)
	}

	_, err = tool.write(context.Background(), &WriteInput{FilePath: path, Content: "hello"})
	if err != nil {
		t.Fatalf("write new file failed: %v", err)
	}
}
