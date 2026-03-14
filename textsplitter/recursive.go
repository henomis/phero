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

var (
	defaultSeparators                 = []string{"\n\n", "\n", " ", ""}
	defaultLengthFunction LenFunction = func(s string) int { return len(s) }
)

// RecursiveCharacterTextSplitter splits text recursively based on a list of separators.
type RecursiveCharacterTextSplitter struct {
	TextSplitter
	separators []string
}

// NewRecursiveCharacterTextSplitter constructs a RecursiveCharacterTextSplitter.
//
// chunkSize and chunkOverlap are interpreted using the configured length
// function (bytes by default). The splitter uses a default set of separators
// (double-newline, newline, space, and "") unless overridden via
// WithSeparators.
func NewRecursiveCharacterTextSplitter(chunkSize, chunkOverlap int) *RecursiveCharacterTextSplitter {
	return &RecursiveCharacterTextSplitter{
		TextSplitter: TextSplitter{
			chunkSize:      chunkSize,
			chunkOverlap:   chunkOverlap,
			lengthFunction: defaultLengthFunction,
		},
		separators: defaultSeparators,
	}
}

// WithSeparators overrides the ordered list of candidate separators.
//
// The splitter will pick the first separator that exists in the input text,
// falling back to the last one.
func (r *RecursiveCharacterTextSplitter) WithSeparators(separators []string) *RecursiveCharacterTextSplitter {
	r.separators = separators
	return r
}

// WithLengthFunction sets the function used to measure text length.
//
// This affects chunkSize, chunkOverlap, and recursion decisions.
func (r *RecursiveCharacterTextSplitter) WithLengthFunction(
	lengthFunction LenFunction,
) *RecursiveCharacterTextSplitter {
	r.lengthFunction = lengthFunction
	return r
}

// SplitText splits text into overlapping chunks.
//
// The input is recursively split by progressively smaller separators until
// chunks are smaller than the configured chunkSize, after which adjacent splits
// are merged into chunks with up to chunkOverlap overlap.
func (r *RecursiveCharacterTextSplitter) SplitText(text string) []string {
	// Split incoming text and return chunks.
	finalChunks := []string{}
	// Get appropriate separator to use
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
	// Now that we have the separator, split the text
	splits := strings.Split(text, separator)
	// Now go merging things, recursively splitting longer texts.
	goodSplits := []string{}
	for _, s := range splits {
		if r.lengthFunction(s) < r.chunkSize {
			goodSplits = append(goodSplits, s)
		} else {
			if len(goodSplits) > 0 {
				mergedText := r.mergeSplits(goodSplits, separator)
				finalChunks = append(finalChunks, mergedText...)
				goodSplits = []string{}
			}
			if len(newSeparators) == 0 {
				finalChunks = append(finalChunks, s)
			} else {
				otherInfo := r.SplitText(s)
				finalChunks = append(finalChunks, otherInfo...)
			}
		}
	}
	if len(goodSplits) > 0 {
		mergedText := r.mergeSplits(goodSplits, separator)
		finalChunks = append(finalChunks, mergedText...)
	}
	return finalChunks
}
