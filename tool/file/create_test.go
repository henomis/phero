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
"strings"
"testing"
)

func TestCreateFile_Success(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewCreateFileTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}

	out, err := tool.write(context.Background(), &CreateFileInput{
		Path:    "hello.txt",
		Content: "hello world",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if out.Len != len("hello world") {
		t.Fatalf("expected len %d, got %d", len("hello world"), out.Len)
	}

	got, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}

func TestCreateFile_NilInput(t *testing.T) {
	tool, err := NewCreateFileTool()
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}
	_, err = tool.write(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil input")
	}
}

func TestCreateFile_EmptyPath(t *testing.T) {
	tool, err := NewCreateFileTool()
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}
	_, err = tool.write(context.Background(), &CreateFileInput{Path: "  ", Content: "x"})
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestCreateFile_PathTraversal_RelativeEscape(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewCreateFileTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}

	_, err = tool.write(context.Background(), &CreateFileInput{
		Path:    "../../evil.txt",
		Content: "pwned",
	})
	if err == nil {
		t.Fatal("expected error for traversal path, got nil")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected 'outside' in error, got: %v", err)
	}
}

func TestCreateFile_PathTraversal_AbsoluteEscape(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewCreateFileTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}

	_, err = tool.write(context.Background(), &CreateFileInput{
		Path:    "/tmp/evil.txt",
		Content: "pwned",
	})
	if err == nil {
		t.Fatal("expected error for absolute path outside workingDir, got nil")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected 'outside' in error, got: %v", err)
	}
}
