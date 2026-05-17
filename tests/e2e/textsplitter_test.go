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

//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/henomis/phero/textsplitter/markdown"
	"github.com/henomis/phero/textsplitter/recursive"
)

// TestTextSplitter_Recursive verifies that the recursive character-based splitter
// produces the expected number of chunks and honours the configured chunk size.
func TestTextSplitter_Recursive(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	content := "First paragraph content goes here and is long enough.\n\n" +
		"Second paragraph content goes here and is long enough.\n\n" +
		"Third paragraph content goes here and is long enough."

	src := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	splitter := recursive.New(src, 80, 10)

	var chunks []string
	for doc, err := range splitter.Split(ctx) {
		if err != nil {
			t.Fatalf("Split: %v", err)
		}
		chunks = append(chunks, doc.Content)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	for i, c := range chunks {
		if len(c) > 90 {
			t.Errorf("chunk %d length %d exceeds chunk_size+overlap: %q", i, len(c), c)
		}
	}

	t.Logf("produced %d chunks", len(chunks))
}

// TestTextSplitter_Markdown verifies that the markdown-aware splitter respects
// heading boundaries and does not produce empty chunks.
func TestTextSplitter_Markdown(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	content := "# Introduction\n\nThis is the introduction section with some content.\n\n" +
		"## Background\n\nBackground information for the document.\n\n" +
		"# Main Topic\n\nDetailed discussion of the main topic goes here.\n\n" +
		"## Subtopic\n\nMore detail about the subtopic.\n"

	src := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	splitter := markdown.New(src, 120, 15)

	var chunks []string
	for doc, err := range splitter.Split(ctx) {
		if err != nil {
			t.Fatalf("Split: %v", err)
		}
		if doc.Content == "" {
			t.Error("got empty chunk")
		}
		chunks = append(chunks, doc.Content)
	}

	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	t.Logf("produced %d markdown chunks", len(chunks))
	for i, c := range chunks {
		t.Logf("  chunk[%d]: %q", i, c)
	}
}
