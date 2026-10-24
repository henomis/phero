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
	"time"
)

func TestMultiEdit_AppliesInOrder(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTree(t, root, map[string]string{"main.go": "a := 1\nb := 2\n"})

	tool, err := NewMultiEditTool(WithWorkingDirectory(root))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.multiEdit(context.Background(), &MultiEditInput{FilePath: path, Edits: []EditOperation{
		{OldString: "a := 1", NewString: "a := 10"},
		{OldString: "a := 10\nb", NewString: "a := 10\nc"}, // sees the first edit
	}})
	if err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(path); string(data) != "a := 10\nc := 2\n" || out.Replacements != 2 {
		t.Fatalf("file = %q, replacements = %d", data, out.Replacements)
	}
}

func TestMultiEdit_AllOrNothing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTree(t, root, map[string]string{"main.go": "a := 1\n"})

	tool, err := NewMultiEditTool(WithWorkingDirectory(root))
	if err != nil {
		t.Fatal(err)
	}

	_, err = tool.multiEdit(context.Background(), &MultiEditInput{FilePath: path, Edits: []EditOperation{
		{OldString: "a := 1", NewString: "a := 2"},
		{OldString: "missing", NewString: "x"},
	}})
	if err == nil {
		t.Fatal("want the failing edit's error")
	}

	if data, _ := os.ReadFile(path); string(data) != "a := 1\n" {
		t.Fatalf("file = %q, want it unchanged", data)
	}
}

func TestReadTracker_RequiresReadBeforeChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTree(t, root, map[string]string{"main.go": "a := 1\n"})

	tracker := NewReadTracker()
	opts := []Option{WithWorkingDirectory(root), WithReadTracker(tracker)}

	read, _ := NewReadTool(opts...)
	write, _ := NewWriteTool(opts...)
	edit, _ := NewEditTool(opts...)
	ctx := context.Background()

	if _, err := edit.edit(ctx, &EditInput{FilePath: path, OldString: "1", NewString: "2"}); !errors.Is(err, ErrFileNotRead) {
		t.Fatalf("edit before read: err = %v, want ErrFileNotRead", err)
	}

	if _, err := write.write(ctx, &WriteInput{FilePath: filepath.Join(root, "new.go"), Content: "x"}); err != nil {
		t.Fatalf("a new file needs no read: %v", err)
	}

	if _, err := read.read(ctx, &ReadInput{FilePath: path}); err != nil {
		t.Fatal(err)
	}

	if _, err := edit.edit(ctx, &EditInput{FilePath: path, OldString: "1", NewString: "2"}); err != nil {
		t.Fatalf("edit after read: %v", err)
	}

	// Its own change does not count as a change behind the model's back.
	if _, err := edit.edit(ctx, &EditInput{FilePath: path, OldString: "2", NewString: "3"}); err != nil {
		t.Fatalf("second edit: %v", err)
	}

	// Someone else changes the file.
	later := time.Now().Add(time.Minute)

	if err := os.WriteFile(path, []byte("changed elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}

	var changed *ChangedError
	if _, err := write.write(ctx, &WriteInput{FilePath: path, Content: "x"}); !errors.As(err, &changed) {
		t.Fatalf("write after an outside change: err = %v, want ChangedError", err)
	}
}

func TestRead_DefaultMaxFileSize(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "big.bin")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	if err = f.Truncate(DefaultMaxFileSize + 1); err != nil {
		t.Fatal(err)
	}

	_ = f.Close()

	read, _ := NewReadTool(WithWorkingDirectory(root))
	if _, err = read.read(context.Background(), &ReadInput{FilePath: path}); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("err = %v, want ErrFileTooLarge by default", err)
	}

	unlimited, _ := NewReadTool(WithWorkingDirectory(root), WithMaxFileSize(0))
	if _, err = unlimited.read(context.Background(), &ReadInput{FilePath: path, Limit: 1}); err != nil {
		t.Fatalf("WithMaxFileSize(0): %v", err)
	}
}
