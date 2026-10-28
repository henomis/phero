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
	"slices"
	"strings"
	"testing"
)

func TestGrepTool_DefaultModeFilesWithMatches(t *testing.T) {
	tmp := t.TempDir()
	matchFile := filepath.Join(tmp, "one.txt")
	missFile := filepath.Join(tmp, "two.txt")

	if err := os.WriteFile(matchFile, []byte("hello gopher"), 0o644); err != nil {
		t.Fatalf("write match file: %v", err)
	}

	if err := os.WriteFile(missFile, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("write miss file: %v", err)
	}

	tool, err := NewGrepTool(WithWorkingDirectory(tmp))
	if err != nil {
		t.Fatalf("new grep tool: %v", err)
	}

	out, err := tool.grep(context.Background(), &GrepInput{Pattern: "gopher", Path: tmp})
	if err != nil {
		t.Fatalf("grep failed: %v", err)
	}

	if len(out.Output) != 1 || out.Output[0] != matchFile {
		t.Fatalf("unexpected grep output: %v", out.Output)
	}
}

func TestGrepTool_ContentModeWithLineNumbers(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "code.go")

	content := strings.Join([]string{"package main", "func main() {}", "// TODO"}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool, err := NewGrepTool(WithWorkingDirectory(tmp))
	if err != nil {
		t.Fatalf("new grep tool: %v", err)
	}

	out, err := tool.grep(context.Background(), &GrepInput{Pattern: "TODO", Path: tmp, OutputMode: "content", N: true})
	if err != nil {
		t.Fatalf("grep content failed: %v", err)
	}

	if len(out.Output) != 1 {
		t.Fatalf("expected 1 output line, got %v", out.Output)
	}

	if !strings.Contains(out.Output[0], ":3:") {
		t.Fatalf("expected line number in output, got %v", out.Output)
	}
}

func TestGrepTool_CountMode(t *testing.T) {
	tmp := t.TempDir()

	path := filepath.Join(tmp, "count.txt")
	if err := os.WriteFile(path, []byte("x\nx\ny"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool, err := NewGrepTool(WithWorkingDirectory(tmp))
	if err != nil {
		t.Fatalf("new grep tool: %v", err)
	}

	out, err := tool.grep(context.Background(), &GrepInput{Pattern: "x", Path: tmp, OutputMode: "count"})
	if err != nil {
		t.Fatalf("grep count failed: %v", err)
	}

	if len(out.Output) != 1 || !strings.HasSuffix(out.Output[0], ":2") {
		t.Fatalf("unexpected count output: %v", out.Output)
	}
}

// TestGrepSchema_OutputModeEnum checks that output_mode lists its values as an
// enum and keeps its full description.
func TestGrepSchema_OutputModeEnum(t *testing.T) {
	tool, err := NewGrepTool()
	if err != nil {
		t.Fatalf("new grep tool: %v", err)
	}

	props, _ := tool.Tool().InputSchema()["properties"].(map[string]any)
	mode, _ := props["output_mode"].(map[string]any)

	if got := fmt.Sprint(mode["enum"]); got != "[content files_with_matches count]" {
		t.Fatalf("enum = %s", got)
	}

	if got, want := mode["description"], "Output mode: content, files_with_matches or count. Defaults to files_with_matches"; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}

	typ, _ := props["type"].(map[string]any)
	if got, want := typ["description"], "File type to search (go, js, py, etc.)"; got != want {
		t.Fatalf("type description = %q, want %q", got, want)
	}
}

func TestGrepTool_HeadLimitOnlyLowersTheLimit(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{"a.txt": strings.Repeat("match\n", 10)})

	tool, err := NewGrepTool(WithWorkingDirectory(tmp), WithMaxResults(3))
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct{ headLimit, want int }{{0, 3}, {2, 2}, {50, 3}} {
		out, grepErr := tool.grep(context.Background(), &GrepInput{
			Pattern: "match", Path: tmp, OutputMode: grepModeContent, HeadLimit: tt.headLimit,
		})
		if grepErr != nil {
			t.Fatal(grepErr)
		}

		if len(out.Output) != tt.want || !out.Truncated {
			t.Fatalf("head_limit %d: %d entries, truncated %v; want %d, true",
				tt.headLimit, len(out.Output), out.Truncated, tt.want)
		}
	}
}

func TestGrepTool_OutputCap(t *testing.T) {
	tmp := t.TempDir()
	writeTree(t, tmp, map[string]string{"a.txt": strings.Repeat("match this line\n", 500)})

	tool, err := NewGrepTool(WithWorkingDirectory(tmp), WithMaxOutputChars(1000))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.grep(context.Background(), &GrepInput{Pattern: "match", Path: tmp, OutputMode: grepModeContent})
	if err != nil {
		t.Fatal(err)
	}

	size := 0
	for _, entry := range out.Output {
		size += len(entry) + 1
	}

	if !out.Truncated || size > 1000 || len(out.Output) == 0 {
		t.Fatalf("%d entries, %d bytes, truncated %v; want a cut result within 1000 bytes", len(out.Output), size, out.Truncated)
	}
}

func TestCapEntries(t *testing.T) {
	tests := []struct {
		name      string
		entries   []string
		max       int
		want      []string
		truncated bool
	}{
		{name: "fits", entries: []string{"ab", "cd"}, max: 6, want: []string{"ab", "cd"}},
		{name: "drops whole entries", entries: []string{"ab", "cd", "ef"}, max: 7, want: []string{"ab", "cd"}, truncated: true},
		{name: "cuts a long first entry", entries: []string{"abcdef", "g"}, max: 4, want: []string{"abcd"}, truncated: true},
		{name: "never splits a character", entries: []string{"aé"}, max: 2, want: []string{"a"}, truncated: true},
		{name: "no cap", entries: []string{"abcdef"}, max: 0, want: []string{"abcdef"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated := capEntries(tt.entries, tt.max)
			if !slices.Equal(got, tt.want) || truncated != tt.truncated {
				t.Fatalf("capEntries = %q, %v; want %q, %v", got, truncated, tt.want, tt.truncated)
			}
		})
	}
}
