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
	"strings"
	"testing"
)

func TestStrReplace_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello OLD world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	out, err := tool.replace(context.Background(), &StrReplaceInput{
		Description: "test",
		Path:        path,
		OldStr:      "OLD",
		NewStr:      "NEW",
	})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if out == nil || out.Replaced != true {
		t.Fatalf("unexpected result: %+v", out)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello NEW world" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}

func TestStrReplace_NilInput(t *testing.T) {
	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestStrReplace_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   path,
		OldStr: "MISSING",
		NewStr: "X",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStrReplace_MultipleMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("X OLD Y OLD Z"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   path,
		OldStr: "OLD",
		NewStr: "NEW",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "found") || !strings.Contains(err.Error(), "times") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStrReplace_OverlappingMatches_CountsAsMultiple(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("aaa"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tool, err := NewStrReplaceTool()
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   path,
		OldStr: "aa",
		NewStr: "X",
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "found") || !strings.Contains(err.Error(), "times") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStrReplace_PathTraversal_RelativeEscape(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewStrReplaceTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   "../../etc/passwd",
		OldStr: "root",
		NewStr: "pwned",
	})
	if err == nil {
		t.Fatal("expected error for traversal path, got nil")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected 'outside' in error, got: %v", err)
	}
}

func TestStrReplace_PathTraversal_AbsoluteEscape(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewStrReplaceTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	// An absolute path outside workingDir must be rejected too.
	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   "/etc/passwd",
		OldStr: "root",
		NewStr: "pwned",
	})
	if err == nil {
		t.Fatal("expected error for absolute path outside workingDir, got nil")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected 'outside' in error, got: %v", err)
	}
}

func TestStrReplace_PathTraversal_SymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(dir, "escape.txt")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	tool, err := NewStrReplaceTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewStrReplaceTool: %v", err)
	}

	_, err = tool.replace(context.Background(), &StrReplaceInput{
		Path:   "escape.txt",
		OldStr: "secret",
		NewStr: "pwned",
	})
	if err == nil {
		t.Fatal("expected error for symlink escape, got nil")
	}
	if !errors.Is(err, ErrPathOutsideWorkingDirectory) {
		t.Fatalf("expected ErrPathOutsideWorkingDirectory, got: %v", err)
	}

	content, readErr := os.ReadFile(outsideFile)
	if readErr != nil {
		t.Fatalf("read outside file: %v", readErr)
	}
	if string(content) != "secret" {
		t.Fatalf("outside file was modified: %q", string(content))
	}
}
