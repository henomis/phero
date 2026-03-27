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
	"context"
	"iter"

	"github.com/henomis/phero/document"
)

// Splitter is the interface implemented by all text-splitting strategies.
//
// Split returns a lazy iterator that yields (Document, error) pairs from the
// splitter's configured source. The source is bound at construction time.
// Callers should stop iteration on the first non-nil error.
type Splitter interface {
	Split(ctx context.Context) iter.Seq2[document.Document, error]
}

// LenFunction returns the length of a string as used by a splitter.
//
// It is used to measure chunk sizes and overlaps; callers can provide a
// rune-counting implementation when working with multi-byte characters.
type LenFunction func(string) int
