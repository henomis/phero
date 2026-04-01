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

func TestCreateFile_PathTraversal_SymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	tool, err := NewCreateFileTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}

	_, err = tool.write(context.Background(), &CreateFileInput{
		Path:    filepath.Join("escape", "evil.txt"),
		Content: "pwned",
	})
	if err == nil {
		t.Fatal("expected error for symlink escape, got nil")
	}
	if !errors.Is(err, ErrPathOutsideWorkingDirectory) {
		t.Fatalf("expected ErrPathOutsideWorkingDirectory, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "evil.txt")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no file to be created outside workingDir, stat err: %v", statErr)
	}
}

func TestCreateFile_NoOverwrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	tool, err := NewCreateFileTool(WithWorkingDirectory(dir), WithNoOverwrite())
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}

	out, err := tool.write(context.Background(), &CreateFileInput{
		Path:    "new.txt",
		Content: "hello",
	})
	if err != nil {
		t.Fatalf("expected success for new file, got: %v", err)
	}
	if out.Len != len("hello") {
		t.Fatalf("unexpected len: %d", out.Len)
	}
}

func TestCreateFile_NoOverwrite_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool, err := NewCreateFileTool(WithWorkingDirectory(dir), WithNoOverwrite())
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}

	_, err = tool.write(context.Background(), &CreateFileInput{
		Path:    "existing.txt",
		Content: "overwritten",
	})
	if err == nil {
		t.Fatal("expected ErrFileExists, got nil")
	}
	if !errors.Is(err, ErrFileExists) {
		t.Fatalf("expected ErrFileExists, got: %v", err)
	}

	// original content must be unchanged
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read file: %v", readErr)
	}
	if string(got) != "original" {
		t.Fatalf("file was overwritten: %q", string(got))
	}
}

func TestCreateFile_DefaultOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	tool, err := NewCreateFileTool(WithWorkingDirectory(dir))
	if err != nil {
		t.Fatalf("NewCreateFileTool: %v", err)
	}

	_, err = tool.write(context.Background(), &CreateFileInput{
		Path:    "file.txt",
		Content: "new",
	})
	if err != nil {
		t.Fatalf("expected overwrite to succeed, got: %v", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read file: %v", readErr)
	}
	if string(got) != "new" {
		t.Fatalf("unexpected content: %q", string(got))
	}
}
