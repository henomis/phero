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
