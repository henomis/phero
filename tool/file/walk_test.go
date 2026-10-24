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
	"slices"
	"strings"
	"testing"
)

// writeTree creates files under root; content "" writes "match".
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if content == "" {
			content = "match"
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// globRel runs glob over root and returns the matched paths relative to root.
func globRel(t *testing.T, root, pattern string, opts ...Option) ([]string, bool) {
	t.Helper()

	tool, err := NewGlobTool(append([]Option{WithWorkingDirectory(root)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.glob(context.Background(), &GlobInput{Pattern: pattern, Path: root})
	if err != nil {
		t.Fatal(err)
	}

	rel := make([]string, 0, len(out.Paths))
	for _, p := range out.Paths {
		r, _ := filepath.Rel(root, p)
		rel = append(rel, filepath.ToSlash(r))
	}

	slices.Sort(rel)

	return rel, out.Truncated
}

func TestGlob_RespectsGitignore(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/HEAD":             "ref",
		".gitignore":            "*.log\n!keep.log\nbuild/\n/top.txt\n# comment\n",
		"a.go":                  "",
		"debug.log":             "",
		"keep.log":              "",
		"top.txt":               "",
		"sub/top.txt":           "",
		"build/out.go":          "",
		"sub/build":             "a file named build is not a directory",
		"node_modules/x/i.js":   "",
		"sub/.gitignore":        "secret.go\n",
		"sub/secret.go":         "",
		"sub/visible.go":        "",
		"other/secret.go":       "",
		"deep/nested/trace.log": "",
	})

	got, _ := globRel(t, root, "**/*")
	want := []string{".gitignore", "a.go", "keep.log", "other/secret.go", "sub/.gitignore", "sub/build", "sub/top.txt", "sub/visible.go"}

	if !slices.Equal(got, want) {
		t.Fatalf("glob = %v\nwant   %v", got, want)
	}
}

func TestGlob_GitignoreAboveSearchRoot(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		".git/HEAD":          "ref",
		".gitignore":         "generated/\n",
		"pkg/a.go":           "",
		"pkg/generated/b.go": "",
	})

	// Search from pkg: the .gitignore that applies sits above the search root.
	got, _ := globRel(t, filepath.Join(root, "pkg"), "**/*.go")
	if !slices.Equal(got, []string{"a.go"}) {
		t.Fatalf("glob = %v, want the repository's .gitignore applied below the search root", got)
	}
}

func TestGlob_NoIgnoreAndCap(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{".gitignore": "*.log\n", "a.log": "", "b.log": "", "node_modules/c.log": ""})

	got, _ := globRel(t, root, "**/*.log", WithNoIgnore())
	if len(got) != 3 {
		t.Fatalf("glob = %v, want everything with WithNoIgnore", got)
	}

	got, truncated := globRel(t, root, "**/*.log", WithNoIgnore(), WithMaxResults(2))
	if len(got) != 2 || !truncated {
		t.Fatalf("glob = %v, truncated = %v; want 2 and truncated", got, truncated)
	}
}

func TestGrep_SkipsBinaryAndCaps(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"text.txt":    "needle\nneedle\nneedle\n",
		"blob.bin":    "needle\x00\x01\x02",
		"long.js":     strings.Repeat("x", 2000) + "needle",
		".gitignore":  "ignored.txt\n",
		"ignored.txt": "needle\n",
	})

	tool, err := NewGrepTool(WithWorkingDirectory(root), WithMaxResults(2))
	if err != nil {
		t.Fatal(err)
	}

	files, err := tool.grep(context.Background(), &GrepInput{Pattern: "needle", Path: root})
	if err != nil {
		t.Fatal(err)
	}

	if len(files.Output) != 2 || files.Truncated {
		t.Fatalf("files = %+v, want long.js and text.txt only", files)
	}

	for _, f := range files.Output {
		if strings.HasSuffix(f, "blob.bin") || strings.HasSuffix(f, "ignored.txt") {
			t.Fatalf("files = %v, want the binary and ignored files skipped", files.Output)
		}
	}

	content, err := tool.grep(context.Background(), &GrepInput{Pattern: "needle", Path: root, OutputMode: grepModeContent})
	if err != nil {
		t.Fatal(err)
	}

	if len(content.Output) != 2 || !content.Truncated {
		t.Fatalf("content = %d entries, truncated = %v; want 2 and truncated", len(content.Output), content.Truncated)
	}

	if l := len(content.Output[0]); l > maxLineRunes+len(root)+50 {
		t.Fatalf("a %d-byte line was not clipped", l)
	}
}

func TestGrep_SkipsSymlinkOutsideWorkingDir(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeTree(t, outside, map[string]string{"secret.txt": "needle"})
	writeTree(t, root, map[string]string{"inside.txt": "needle"})

	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	tool, err := NewGrepTool(WithWorkingDirectory(root))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.grep(context.Background(), &GrepInput{Pattern: "needle", Path: root})
	if err != nil {
		t.Fatal(err)
	}

	if len(out.Output) != 1 || !strings.HasSuffix(out.Output[0], "inside.txt") {
		t.Fatalf("output = %v, want only the file inside the working directory", out.Output)
	}
}
