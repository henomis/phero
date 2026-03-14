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

package textsplitter

import (
	"reflect"
	"testing"
)

func assertStringsSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("unexpected chunks:\n got: %#v\nwant: %#v", got, want)
}

func TestRecursiveCharacterTextSplitter_SplitText_SpaceSeparator_NoOverlap(t *testing.T) {
	s := NewRecursiveCharacterTextSplitter(10, 0).WithSeparators([]string{" "})

	got := s.SplitText("hello world")
	want := []string{"hello", "world"}

	assertStringsSliceEqual(t, got, want)
}

func TestRecursiveCharacterTextSplitter_SplitText_SpaceSeparator_WithOverlap(t *testing.T) {
	s := NewRecursiveCharacterTextSplitter(10, 4).WithSeparators([]string{" "})

	got := s.SplitText("aa bb cc dd ee")
	want := []string{"aa bb cc", "cc dd ee"}

	assertStringsSliceEqual(t, got, want)
}

func TestRecursiveCharacterTextSplitter_WithLengthFunction_ChangesChunking(t *testing.T) {
	text := "aaaaa b"

	defaultSplitter := NewRecursiveCharacterTextSplitter(3, 0).WithSeparators([]string{" "})
	gotDefault := defaultSplitter.SplitText(text)
	wantDefault := []string{"aaaaa", "b"}
	assertStringsSliceEqual(t, gotDefault, wantDefault)

	tokenLenSplitter := NewRecursiveCharacterTextSplitter(3, 0).
		WithSeparators([]string{" "}).
		WithLengthFunction(func(string) int { return 1 })
	gotTokenLen := tokenLenSplitter.SplitText(text)
	wantTokenLen := []string{"aaaaa b"}
	assertStringsSliceEqual(t, gotTokenLen, wantTokenLen)
}

func TestRecursiveCharacterTextSplitter_WithSeparators_CustomSeparator(t *testing.T) {
	s := NewRecursiveCharacterTextSplitter(3, 0).WithSeparators([]string{"|"})

	got := s.SplitText("a|b|c|d")
	want := []string{"a|b", "c|d"}

	assertStringsSliceEqual(t, got, want)
}

func TestRecursiveCharacterTextSplitter_SplitText_EmptyInput(t *testing.T) {
	s := NewRecursiveCharacterTextSplitter(10, 0).WithSeparators([]string{" "})

	got := s.SplitText("")
	want := []string{}

	assertStringsSliceEqual(t, got, want)
}
