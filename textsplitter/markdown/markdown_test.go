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

package markdown

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/henomis/phero/textsplitter"
)

func TestSplitter_ImplementsSplitter(t *testing.T) {
	var _ textsplitter.Splitter = New("", 100, 0)
}

func TestSplitter_Split_ByHeadings(t *testing.T) {
	const input = "# Title\n## Section One\nContent of section one.\n## Section Two\nContent of section two."
	path := t.TempDir() + "/doc.md"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	s := New(path, 500, 0)

	var chunks []string
	for doc, iterErr := range s.Split(context.TODO()) {
		if iterErr != nil {
			t.Fatalf("unexpected error: %v", iterErr)
		}
		chunks = append(chunks, doc.Content)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk, got none")
	}

	full := strings.Join(chunks, " ")
	for _, needle := range []string{"Section One", "Section Two"} {
		if !strings.Contains(full, needle) {
			t.Errorf("expected output to contain %q", needle)
		}
	}
}

func TestSplitter_Split_FallsBackToNewline(t *testing.T) {
	const input = "Line one.\nLine two.\nLine three."
	path := t.TempDir() + "/doc.md"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	s := New(path, 15, 0)

	var chunks []string
	for doc, iterErr := range s.Split(context.TODO()) {
		if iterErr != nil {
			t.Fatalf("unexpected error: %v", iterErr)
		}
		chunks = append(chunks, doc.Content)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for text with newlines, got %d: %v", len(chunks), chunks)
	}
}

func TestSplitter_Split_LargeSection_FurtherSplit(t *testing.T) {
	body := strings.Repeat("word ", 50)
	input := "## Heading\n" + body
	path := t.TempDir() + "/doc.md"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	s := New(path, 50, 0)

	var chunks []string
	for doc, iterErr := range s.Split(context.TODO()) {
		if iterErr != nil {
			t.Fatalf("unexpected error: %v", iterErr)
		}
		chunks = append(chunks, doc.Content)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected large section to be further split, got %d chunks", len(chunks))
	}
	for _, chunk := range chunks {
		if len(chunk) > 55 {
			t.Errorf("chunk exceeds chunkSize: %q (len=%d)", chunk, len(chunk))
		}
	}
}

func TestSplitter_Split_EmptyInput(t *testing.T) {
	path := t.TempDir() + "/empty.md"
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	s := New(path, 100, 0)

	var chunks []string
	for doc, iterErr := range s.Split(context.TODO()) {
		if iterErr != nil {
			t.Fatalf("unexpected error: %v", iterErr)
		}
		chunks = append(chunks, doc.Content)
	}

	if len(chunks) != 0 {
		t.Fatalf("expected no chunks for empty input, got %v", chunks)
	}
}
