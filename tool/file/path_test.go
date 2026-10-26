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
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePath(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	writeTree(t, root, map[string]string{"main.go": "package main\n"})

	if err = os.Symlink(filepath.Join(root, "main.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}

	if err = os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want string
		err  error
	}{
		{name: "file", path: filepath.Join(root, "main.go"), want: filepath.Join(realRoot, "main.go")},
		{name: "symlink", path: filepath.Join(root, "link.go"), want: filepath.Join(realRoot, "main.go")},
		{name: "missing file", path: filepath.Join(root, "new", "x.go"), want: filepath.Join(realRoot, "new", "x.go")},
		{name: "relative", path: "main.go", err: ErrPathMustBeAbsolute},
		{name: "dot dot", path: filepath.Join(root, "..", "x.go"), err: ErrPathOutsideWorkingDirectory},
		{name: "symlink out", path: filepath.Join(root, "escape", "x.go"), err: ErrPathOutsideWorkingDirectory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, resolveErr := ResolvePath(root, tt.path)
			if tt.err != nil {
				if !errors.Is(resolveErr, tt.err) {
					t.Fatalf("err = %v, want %v", resolveErr, tt.err)
				}

				return
			}

			if resolveErr != nil || got != tt.want {
				t.Fatalf("ResolvePath = %q, %v; want %q", got, resolveErr, tt.want)
			}
		})
	}
}

func TestResolvePath_NoWorkingDirectory(t *testing.T) {
	root := t.TempDir()

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}

	writeTree(t, root, map[string]string{"dir/main.go": "package main\n"})

	if err = os.Symlink(filepath.Join(root, "dir"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "symlinked parent", path: filepath.Join(root, "link", "main.go"), want: filepath.Join(realRoot, "dir", "main.go")},
		{name: "missing file under a symlink", path: filepath.Join(root, "link", "new", "x.go"), want: filepath.Join(realRoot, "dir", "new", "x.go")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, resolveErr := ResolvePath("", tt.path)
			if resolveErr != nil || got != tt.want {
				t.Fatalf("ResolvePath = %q, %v; want %q", got, resolveErr, tt.want)
			}
		})
	}

	if _, err = ResolvePath("", "main.go"); !errors.Is(err, ErrPathMustBeAbsolute) {
		t.Fatalf("relative path: err = %v, want ErrPathMustBeAbsolute", err)
	}
}
