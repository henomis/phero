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

func TestEditTool_RequiresRead(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "edit.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	session := NewSession()
	tool, err := NewEditTool(WithWorkingDirectory(tmp), WithSession(session))
	if err != nil {
		t.Fatalf("new edit tool: %v", err)
	}

	_, err = tool.edit(context.Background(), &EditInput{FilePath: path, OldString: "world", NewString: "gophers"})
	if !errors.Is(err, ErrReadRequired) {
		t.Fatalf("expected ErrReadRequired, got %v", err)
	}

	session.MarkRead(path)
	out, err := tool.edit(context.Background(), &EditInput{FilePath: path, OldString: "world", NewString: "gophers"})
	if err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	if out.Replacements != 1 {
		t.Fatalf("unexpected replacements: %d", out.Replacements)
	}
}

func TestEditTool_ReplaceAll(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "multi.txt")
	if err := os.WriteFile(path, []byte("x x x"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	session := NewSession()
	session.MarkRead(path)
	tool, err := NewEditTool(WithWorkingDirectory(tmp), WithSession(session))
	if err != nil {
		t.Fatalf("new edit tool: %v", err)
	}

	out, err := tool.edit(context.Background(), &EditInput{FilePath: path, OldString: "x", NewString: "y", ReplaceAll: true})
	if err != nil {
		t.Fatalf("replace_all failed: %v", err)
	}
	if out.Replacements != 3 {
		t.Fatalf("expected 3 replacements, got %d", out.Replacements)
	}
}
