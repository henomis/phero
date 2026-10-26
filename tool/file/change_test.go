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

// realDir returns dir with its symlinks resolved, as the tools resolve paths.
func realDir(t *testing.T, dir string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	return resolved
}

func TestPreview_MatchesTheCallAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTree(t, root, map[string]string{"main.go": "a := 1\nb := 1\n"})

	edit, _ := NewEditTool(WithWorkingDirectory(root))
	multi, _ := NewMultiEditTool(WithWorkingDirectory(root))
	ctx := context.Background()

	change, err := multi.Preview(&MultiEditInput{FilePath: path, Edits: []EditOperation{
		{OldString: "a := 1", NewString: "a := 2"},
		{OldString: "a := 2", NewString: "a := 3"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	if string(change.Before) != "a := 1\nb := 1\n" || string(change.After) != "a := 3\nb := 1\n" || !change.Existed ||
		change.Path != filepath.Join(realDir(t, root), "main.go") || change.Perm != 0o600 {
		t.Fatalf("change = %+v, want the edits applied in order", change)
	}

	if data, _ := os.ReadFile(path); string(data) != "a := 1\nb := 1\n" {
		t.Fatalf("file = %q, want it unchanged by Preview", data)
	}

	input := &EditInput{FilePath: path, OldString: "1", NewString: "2", ReplaceAll: true}

	change, err = edit.Preview(input)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = edit.edit(ctx, input); err != nil {
		t.Fatal(err)
	}

	if data, _ := os.ReadFile(path); string(data) != string(change.After) {
		t.Fatalf("file = %q, want the previewed %q", data, change.After)
	}
}

func TestPreview_FailsAsTheCallWould(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTree(t, root, map[string]string{"main.go": "a := 1\na := 1\n"})

	edit, _ := NewEditTool(WithWorkingDirectory(root))
	small, _ := NewEditTool(WithWorkingDirectory(root), WithMaxFileSize(4))
	tracked, _ := NewEditTool(WithWorkingDirectory(root), WithReadTracker(NewReadTracker()))
	multi, _ := NewMultiEditTool(WithWorkingDirectory(root))
	write, _ := NewWriteTool(WithWorkingDirectory(root), WithNoOverwrite())

	tests := []struct {
		name    string
		preview func() (*Change, error)
		want    string
		is      error
	}{
		{name: "same string", preview: func() (*Change, error) {
			return edit.Preview(&EditInput{FilePath: path, OldString: "a", NewString: "a"})
		}, want: "new_string must differ from old_string"},
		{name: "not unique", preview: func() (*Change, error) {
			return edit.Preview(&EditInput{FilePath: path, OldString: "a := 1", NewString: "x"})
		}, want: "old_string found 2 times in " + filepath.Join(realDir(t, root), "main.go")},
		{name: "too large", preview: func() (*Change, error) {
			return small.Preview(&EditInput{FilePath: path, OldString: "a", NewString: "b", ReplaceAll: true})
		}, is: ErrFileTooLarge},
		{name: "not read", preview: func() (*Change, error) {
			return tracked.Preview(&EditInput{FilePath: path, OldString: "a", NewString: "b", ReplaceAll: true})
		}, is: ErrFileNotRead},
		{name: "outside", preview: func() (*Change, error) {
			return edit.Preview(&EditInput{FilePath: "/etc/passwd", OldString: "a", NewString: "b"})
		}, is: ErrPathOutsideWorkingDirectory},
		{name: "no edits", preview: func() (*Change, error) {
			return multi.Preview(&MultiEditInput{FilePath: path})
		}, is: ErrEditsRequired},
		{name: "no overwrite", preview: func() (*Change, error) {
			return write.Preview(&WriteInput{FilePath: path, Content: "x"})
		}, is: ErrFileExists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change, previewErr := tt.preview()
			if change != nil || previewErr == nil {
				t.Fatalf("Preview = %+v, %v; want an error", change, previewErr)
			}

			if tt.is != nil && !errors.Is(previewErr, tt.is) {
				t.Fatalf("err = %v, want %v", previewErr, tt.is)
			}

			if tt.want != "" && previewErr.Error() != tt.want {
				t.Fatalf("err = %q, want %q", previewErr, tt.want)
			}
		})
	}
}

func TestPreview_Write(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTree(t, root, map[string]string{"main.go": "old\n"})

	write, _ := NewWriteTool(WithWorkingDirectory(root))

	change, err := write.Preview(&WriteInput{FilePath: path, Content: "new\n"})
	if err != nil {
		t.Fatal(err)
	}

	if !change.Existed || string(change.Before) != "old\n" || string(change.After) != "new\n" || change.Perm != 0o600 {
		t.Fatalf("change = %+v, want old to new with the file's permissions", change)
	}

	change, err = write.Preview(&WriteInput{FilePath: filepath.Join(root, "pkg", "new.go"), Content: "x"})
	if err != nil {
		t.Fatal(err)
	}

	if change.Existed || change.Before != nil || change.Perm != defaultFileMode {
		t.Fatalf("change = %+v, want a new file", change)
	}

	if _, err = os.Stat(filepath.Join(root, "pkg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Preview created the directory: %v", err)
	}
}

func TestWithOnWrite_RecordsEachChangeAndRestores(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTree(t, root, map[string]string{"main.go": "a := 1\n"})

	var changes []Change

	opts := []Option{WithWorkingDirectory(root), WithOnWrite(func(_ context.Context, c Change) {
		changes = append(changes, c)
	})}

	write, _ := NewWriteTool(opts...)
	edit, _ := NewEditTool(opts...)
	multi, _ := NewMultiEditTool(opts...)
	ctx := context.Background()
	newPath := filepath.Join(root, "new.go")

	if _, err := write.write(ctx, &WriteInput{FilePath: newPath, Content: "x"}); err != nil {
		t.Fatal(err)
	}

	if _, err := edit.edit(ctx, &EditInput{FilePath: path, OldString: "1", NewString: "2"}); err != nil {
		t.Fatal(err)
	}

	if _, err := edit.edit(ctx, &EditInput{FilePath: path, OldString: "missing", NewString: "x"}); err == nil {
		t.Fatal("want the failing edit's error")
	}

	if _, err := multi.multiEdit(ctx, &MultiEditInput{FilePath: path, Edits: []EditOperation{{OldString: "2", NewString: "3"}}}); err != nil {
		t.Fatal(err)
	}

	if _, err := write.write(ctx, &WriteInput{FilePath: path, Content: "b := 1\n"}); err != nil {
		t.Fatal(err)
	}

	if len(changes) != 4 {
		t.Fatalf("hook called %d times, want 4 (not for the failed edit)", len(changes))
	}

	if c := changes[0]; c.Existed || c.Before != nil || string(c.After) != "x" {
		t.Fatalf("new file change = %+v", c)
	}

	if c := changes[3]; !c.Existed || string(c.Before) != "a := 3\n" || string(c.After) != "b := 1\n" {
		t.Fatalf("overwrite change = %+v, want the old content read for the hook", c)
	}

	// Undo, newest first.
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if !c.Existed {
			if err := os.Remove(c.Path); err != nil {
				t.Fatal(err)
			}

			continue
		}

		if err := WriteFileAtomic(c.Path, c.Before, c.Perm); err != nil {
			t.Fatal(err)
		}
	}

	if data, _ := os.ReadFile(path); string(data) != "a := 1\n" {
		t.Fatalf("file = %q after undo, want the original", data)
	}

	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new file still exists after undo: %v", err)
	}
}
