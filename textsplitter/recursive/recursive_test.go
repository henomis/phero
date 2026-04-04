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

package recursive

import (
	"context"
	"os"
	"reflect"
	"testing"
)

func writeTempFile(t *testing.T, input string) string {
	t.Helper()

	path := t.TempDir() + "/input.txt"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}

	return path
}

func collectChunks(t *testing.T, splitter *Splitter) []string {
	t.Helper()

	var chunks []string
	for doc, iterErr := range splitter.Split(context.TODO()) {
		if iterErr != nil {
			t.Fatalf("unexpected error from Split: %v", iterErr)
		}
		chunks = append(chunks, doc.Content)
	}
	if chunks == nil {
		return []string{}
	}

	return chunks
}

func assertStringsSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}

	t.Fatalf("unexpected chunks:\n got: %#v\nwant: %#v", got, want)
}

func TestSplitter_Split_SpaceSeparator_NoOverlap(t *testing.T) {
	path := writeTempFile(t, "hello world")
	s := New(path, 10, 0).WithSeparators([]string{" "})

	got := collectChunks(t, s)
	want := []string{"hello", "world"}

	assertStringsSliceEqual(t, got, want)
}

func TestSplitter_Split_SpaceSeparator_WithOverlap(t *testing.T) {
	path := writeTempFile(t, "aa bb cc dd ee")
	s := New(path, 10, 4).WithSeparators([]string{" "})

	got := collectChunks(t, s)
	want := []string{"aa bb cc", "cc dd ee"}

	assertStringsSliceEqual(t, got, want)
}

func TestSplitter_WithLengthFunction_ChangesChunking(t *testing.T) {
	text := "aaaaa b"
	path := writeTempFile(t, text)

	defaultSplitter := New(path, 3, 0).WithSeparators([]string{" "})
	gotDefault := collectChunks(t, defaultSplitter)
	wantDefault := []string{"aaaaa", "b"}
	assertStringsSliceEqual(t, gotDefault, wantDefault)

	tokenLenSplitter := New(path, 3, 0).
		WithSeparators([]string{" "}).
		WithLengthFunction(func(string) int { return 1 })
	gotTokenLen := collectChunks(t, tokenLenSplitter)
	wantTokenLen := []string{"aaaaa b"}
	assertStringsSliceEqual(t, gotTokenLen, wantTokenLen)
}

func TestSplitter_WithSeparators_CustomSeparator(t *testing.T) {
	path := writeTempFile(t, "a|b|c|d")
	s := New(path, 3, 0).WithSeparators([]string{"|"})

	got := collectChunks(t, s)
	want := []string{"a|b", "c|d"}

	assertStringsSliceEqual(t, got, want)
}

func TestSplitter_Split_EmptyInput(t *testing.T) {
	path := writeTempFile(t, "")
	s := New(path, 10, 0).WithSeparators([]string{" "})

	got := collectChunks(t, s)
	want := []string{}

	assertStringsSliceEqual(t, got, want)
}

func TestSplitter_Split_Cases(t *testing.T) {
	tests := []struct {
		name         string
		chunkSize    int
		chunkOverlap int
		separators   []string
		input        string
		want         []string
	}{
		{
			name:         "space separator no overlap",
			chunkSize:    10,
			chunkOverlap: 0,
			separators:   []string{" "},
			input:        "hello world",
			want:         []string{"hello", "world"},
		},
		{
			name:         "space separator with overlap",
			chunkSize:    10,
			chunkOverlap: 4,
			separators:   []string{" "},
			input:        "aa bb cc dd ee",
			want:         []string{"aa bb cc", "cc dd ee"},
		},
		{
			name:         "empty input",
			chunkSize:    10,
			chunkOverlap: 0,
			separators:   []string{" "},
			input:        "",
			want:         []string{},
		},
		{
			name:         "default separators multiline",
			chunkSize:    20,
			chunkOverlap: 0,
			separators:   defaultSeparators,
			input:        "paragraph one\n\nparagraph two\n\nparagraph three",
			want:         []string{"paragraph one", "paragraph two", "paragraph three"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTempFile(t, tc.input)
			s := New(path, tc.chunkSize, tc.chunkOverlap).WithSeparators(tc.separators)
			got := collectChunks(t, s)
			assertStringsSliceEqual(t, got, tc.want)
		})
	}
}

func TestSplitter_Split_EarlyStop(t *testing.T) {
	const input = "one two three four five"
	path := writeTempFile(t, input)
	s := New(path, 5, 0).WithSeparators([]string{" "})

	var collected []string
	for doc, iterErr := range s.Split(context.TODO()) {
		if iterErr != nil {
			t.Fatalf("unexpected error: %v", iterErr)
		}
		collected = append(collected, doc.Content)
		if len(collected) == 2 {
			break
		}
	}

	if len(collected) != 2 {
		t.Fatalf("expected 2 chunks before early stop, got %d", len(collected))
	}
}

func TestSplitter_Split_FileNotFound(t *testing.T) {
	s := New("/nonexistent/path/file.txt", 10, 0)

	var gotErr error
	for _, err := range s.Split(context.TODO()) {
		gotErr = err
		break
	}

	if gotErr == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
