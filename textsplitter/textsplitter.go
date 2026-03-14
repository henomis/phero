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
	"strings"
)

// LenFunction returns the length of a string as used by a splitter.
//
// It is used to measure chunk sizes and overlaps; callers can provide a
// rune-counting implementation when working with multi-byte characters.
type LenFunction func(string) int

// TextSplitter contains shared configuration and helpers for splitters.
//
// It is embedded by specific splitter implementations.
type TextSplitter struct {
	chunkSize      int
	chunkOverlap   int
	lengthFunction LenFunction
}

func (t *TextSplitter) mergeSplits(splits []string, separator string) []string {
	docs := make([]string, 0)
	currentDoc := make([]string, 0)
	total := 0
	for _, d := range splits {
		splitLen := t.lengthFunction(d)

		if total+splitLen+getSLen(currentDoc, separator, 0) > t.chunkSize {
			if len(currentDoc) > 0 {
				doc := t.joinDocs(currentDoc, separator)
				if doc != "" {
					docs = append(docs, doc)
				}
				for (total > t.chunkOverlap) || (getSLen(currentDoc, separator, 0) > t.chunkSize) && total > 0 {
					total -= t.lengthFunction(currentDoc[0]) + getSLen(currentDoc, separator, 1)
					currentDoc = currentDoc[1:]
				}
			}
		}
		currentDoc = append(currentDoc, d)
		total += getSLen(currentDoc, separator, 1)
		total += splitLen
	}
	doc := t.joinDocs(currentDoc, separator)
	if doc != "" {
		docs = append(docs, doc)
	}
	return docs
}

func (t *TextSplitter) joinDocs(docs []string, separator string) string {
	text := strings.Join(docs, separator)
	return strings.TrimSpace(text)
}

func getSLen(currentDoc []string, separator string, compareLen int) int {
	if len(currentDoc) > compareLen {
		return len(separator)
	}

	return 0
}
