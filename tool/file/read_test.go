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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTool_ReadAndTrackSession(t *testing.T) {
	tmp := t.TempDir()

	path := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool, err := NewReadTool(WithWorkingDirectory(tmp))
	if err != nil {
		t.Fatalf("new read tool: %v", err)
	}

	out, err := tool.read(context.Background(), &ReadInput{FilePath: path, Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if !strings.Contains(out, "2\tb") {
		t.Fatalf("unexpected read output: %q", out)
	}
}

func TestReadTool_RequiresAbsolutePath(t *testing.T) {
	tool, err := NewReadTool()
	if err != nil {
		t.Fatalf("new read tool: %v", err)
	}

	_, err = tool.read(context.Background(), &ReadInput{FilePath: "relative.txt"})
	if !errors.Is(err, ErrPathMustBeAbsolute) {
		t.Fatalf("expected ErrPathMustBeAbsolute, got %v", err)
	}
}

func writeLines(t *testing.T, path string, n, width int) {
	t.Helper()

	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "%0*d\n", width, i+1)
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestReadTool_OutputCapPointsToOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.txt")
	writeLines(t, path, 1000, 200) // 1000 lines of 200 chars: about 200k chars

	tool, err := NewReadTool(WithMaxOutputChars(10_000))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.read(context.Background(), &ReadInput{FilePath: path})
	if err != nil {
		t.Fatal(err)
	}

	body, note, found := strings.Cut(out, "\n[output cut at")
	if !found || len(body) > 10_000 {
		t.Fatalf("len %d, note found %v: want the output cut under the cap with a note", len(body), found)
	}

	// The note gives the offset to read on from: the first line not shown.
	shown := strings.Count(body, "\n") + 1
	if !strings.Contains(note, fmt.Sprintf("offset=%d", shown)) {
		t.Fatalf("note %q, want offset=%d", note, shown)
	}
}

func TestReadTool_LimitIsCapped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "long.txt")
	writeLines(t, path, defaultReadLimit+500, 1)

	tool, err := NewReadTool(WithMaxOutputChars(0))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.read(context.Background(), &ReadInput{FilePath: path, Limit: 1_000_000})
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Count(out, "\n") + 1; got != defaultReadLimit {
		t.Fatalf("lines = %d, want limit capped at %d", got, defaultReadLimit)
	}
}

func TestReadTool_PagesFilesOverTheSizeCap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.log")
	writeLines(t, path, 5000, 9) // 50 kB

	tool, err := NewReadTool(WithMaxFileSize(10_000))
	if err != nil {
		t.Fatal(err)
	}

	// A whole-file read is still refused...
	var tooLarge *TooLargeError
	if _, err = tool.read(context.Background(), &ReadInput{FilePath: path}); !errors.As(err, &tooLarge) {
		t.Fatalf("err = %v, want a TooLargeError", err)
	}

	// ...but a page of it can be read.
	out, err := tool.read(context.Background(), &ReadInput{FilePath: path, Offset: 4000, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}

	if want := "  4001\t000004001\n  4002\t000004002"; out != want {
		t.Fatalf("content = %q, want %q", out, want)
	}
}

func TestReadTool_PlainText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("func main() {\n\tprintln(\"hi\")\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tool, err := NewReadTool()
	if err != nil {
		t.Fatal(err)
	}

	// Through the tool handler, as the agent calls it: the result must be a
	// string, so the tab and the quotes reach the model unescaped.
	args, err := json.Marshal(ReadInput{FilePath: path})
	if err != nil {
		t.Fatal(err)
	}

	res, err := tool.Tool().Handle(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := res.(string)
	if !ok {
		t.Fatalf("result type %T, want string", res)
	}

	if want := "     1\tfunc main() {\n     2\t\tprintln(\"hi\")\n     3\t}"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}
