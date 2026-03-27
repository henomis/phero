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
	"iter"
	"os"
	"strings"

	"github.com/henomis/phero/document"
	"github.com/henomis/phero/textsplitter"
)

const (
	// MetaSource is the metadata key for the source file path.
	MetaSource = "source"
	// MetaChunkIndex is the metadata key for the zero-based chunk index.
	MetaChunkIndex = "chunk_index"
	// MetaStartOffset is the metadata key for the byte start offset within the source file.
	MetaStartOffset = "start_offset"
	// MetaEndOffset is the metadata key for the byte end offset within the source file.
	MetaEndOffset = "end_offset"
)

var (
	defaultSeparators                              = []string{"\n\n", "\n", " ", ""}
	defaultLengthFunction textsplitter.LenFunction = func(s string) int { return len(s) }
)

// Splitter splits text recursively based on an ordered list of separators.
type Splitter struct {
	source         string
	chunkSize      int
	chunkOverlap   int
	lengthFunction textsplitter.LenFunction
	separators     []string
}

// New constructs a recursive character-based splitter whose source file is
// given by source. The source path is read when Split is called.
func New(source string, chunkSize, chunkOverlap int) *Splitter {
	return &Splitter{
		source:         source,
		chunkSize:      chunkSize,
		chunkOverlap:   chunkOverlap,
		lengthFunction: defaultLengthFunction,
		separators:     defaultSeparators,
	}
}

// WithSeparators overrides the ordered list of candidate separators.
func (r *Splitter) WithSeparators(separators []string) *Splitter {
	r.separators = separators
	return r
}

// WithLengthFunction sets the function used to measure text length.
func (r *Splitter) WithLengthFunction(lengthFunction textsplitter.LenFunction) *Splitter {
	r.lengthFunction = lengthFunction
	return r
}

// Split implements textsplitter.Splitter by reading the configured source file
// and yielding Documents. Each Document carries the chunk text together with
// metadata fields: "source", "chunk_index", "start_offset", and "end_offset".
// Offsets are byte positions within the raw file content; they are set to -1
// when the chunk cannot be located verbatim (e.g. after whitespace trimming).
func (r *Splitter) Split(_ context.Context) iter.Seq2[document.Document, error] {
	return func(yield func(document.Document, error) bool) {
		data, err := os.ReadFile(r.source)
		if err != nil {
			yield(document.Document{}, &ErrReadFile{Source: r.source, Err: err})
			return
		}
		content := string(data)
		cursor := 0
		chunkIndex := 0
		if !r.splitStream(content, func(chunk string) bool {
			startOffset := -1
			endOffset := -1
			if idx := strings.Index(content[cursor:], chunk); idx >= 0 {
				startOffset = cursor + idx
				endOffset = startOffset + len(chunk)
				cursor = startOffset + len(chunk)
			}
			doc := document.Document{
				Content: chunk,
				Metadata: map[string]any{
					MetaSource:      r.source,
					MetaChunkIndex:  chunkIndex,
					MetaStartOffset: startOffset,
					MetaEndOffset:   endOffset,
				},
			}
			chunkIndex++
			return yield(doc, nil)
		}) {
			return
		}
	}
}

func (r *Splitter) splitStream(text string, yield func(string) bool) bool {
	separator := r.separators[len(r.separators)-1]
	newSeparators := []string{}
	for i, s := range r.separators {
		if s == "" {
			separator = s
			break
		}

		if strings.Contains(text, s) {
			separator = s
			newSeparators = r.separators[i+1:]
			break
		}
	}

	splits := strings.Split(text, separator)
	goodSplits := []string{}
	for _, s := range splits {
		if r.lengthFunction(s) < r.chunkSize {
			goodSplits = append(goodSplits, s)
			continue
		}

		if len(goodSplits) > 0 {
			if !r.mergeSplitsStream(goodSplits, separator, yield) {
				return false
			}
			goodSplits = goodSplits[:0]
		}
		if len(newSeparators) == 0 {
			if !yield(s) {
				return false
			}
			continue
		}

		if !r.splitStream(s, yield) {
			return false
		}
	}
	if len(goodSplits) > 0 {
		if !r.mergeSplitsStream(goodSplits, separator, yield) {
			return false
		}
	}

	return true
}

func (r *Splitter) mergeSplitsStream(splits []string, separator string, yield func(string) bool) bool {
	currentDoc := make([]string, 0)
	total := 0
	for _, d := range splits {
		splitLen := r.lengthFunction(d)

		if total+splitLen+separatorLen(currentDoc, separator, 0) > r.chunkSize {
			if len(currentDoc) > 0 {
				doc := r.joinDocs(currentDoc, separator)
				if doc != "" && !yield(doc) {
					return false
				}
				for (total > r.chunkOverlap) || (separatorLen(currentDoc, separator, 0) > r.chunkSize && total > 0) {
					total -= r.lengthFunction(currentDoc[0]) + separatorLen(currentDoc, separator, 1)
					currentDoc = currentDoc[1:]
				}
			}
		}
		currentDoc = append(currentDoc, d)
		total += separatorLen(currentDoc, separator, 1)
		total += splitLen
	}
	doc := r.joinDocs(currentDoc, separator)
	if doc != "" && !yield(doc) {
		return false
	}

	return true
}

func (r *Splitter) joinDocs(docs []string, separator string) string {
	text := strings.Join(docs, separator)
	return strings.TrimSpace(text)
}

func separatorLen(currentDoc []string, separator string, compareLen int) int {
	if len(currentDoc) > compareLen {
		return len(separator)
	}

	return 0
}
