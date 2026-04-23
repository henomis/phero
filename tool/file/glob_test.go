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
