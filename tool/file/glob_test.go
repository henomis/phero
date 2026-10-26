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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGlobTool_MatchesAndSortsByModTime(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	p1 := filepath.Join(tmp, "a.go")
	p2 := filepath.Join(tmp, "nested", "b.go")

	if err := os.WriteFile(p1, []byte("package main"), 0o644); err != nil {
		t.Fatalf("write p1: %v", err)
	}

	if err := os.WriteFile(p2, []byte("package nested"), 0o644); err != nil {
		t.Fatalf("write p2: %v", err)
	}

	now := time.Now()
	if err := os.Chtimes(p1, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes p1: %v", err)
	}

	if err := os.Chtimes(p2, now, now); err != nil {
		t.Fatalf("chtimes p2: %v", err)
	}

	tool, err := NewGlobTool(WithWorkingDirectory(tmp))
	if err != nil {
		t.Fatalf("new glob tool: %v", err)
	}

	out, err := tool.glob(context.Background(), &GlobInput{Pattern: "**/*.go", Path: tmp})
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}

	if len(out.Paths) != 2 {
		t.Fatalf("expected 2 matches, got %d (%v)", len(out.Paths), out.Paths)
	}

	if out.Paths[0] != p2 {
		t.Fatalf("expected newest file first, got %v", out.Paths)
	}
}

func TestGlobTool_OutputCap(t *testing.T) {
	tmp := t.TempDir()

	files := map[string]string{}
	for i := range 100 {
		files[fmt.Sprintf("file-%03d.txt", i)] = "x"
	}

	writeTree(t, tmp, files)

	tool, err := NewGlobTool(WithWorkingDirectory(tmp), WithMaxOutputChars(500))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.glob(context.Background(), &GlobInput{Pattern: "*.txt", Path: tmp})
	if err != nil {
		t.Fatal(err)
	}

	size := 0
	for _, path := range out.Paths {
		size += len(path) + 1
	}

	if !out.Truncated || size > 500 || len(out.Paths) == 0 {
		t.Fatalf("%d paths, %d bytes, truncated %v; want a cut list within 500 bytes", len(out.Paths), size, out.Truncated)
	}
}
