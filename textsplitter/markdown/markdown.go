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
	"github.com/henomis/phero/textsplitter"
	"github.com/henomis/phero/textsplitter/recursive"
)

var separators = []string{
	"\n## ",
	"\n### ",
	"\n#### ",
	"\n##### ",
	"\n###### ",
	"\n\n",
	"\n",
	" ",
	"",
}

// Splitter splits Markdown text by heading levels before falling back to
// paragraph and line boundaries.
type Splitter struct {
	*recursive.Splitter
}

// New constructs a Markdown-aware splitter whose source file is given by source.
func New(source string, chunkSize, chunkOverlap int) *Splitter {
	r := recursive.New(source, chunkSize, chunkOverlap).WithSeparators(separators)
	return &Splitter{Splitter: r}
}

// WithLengthFunction sets the function used to measure text length.
func (s *Splitter) WithLengthFunction(lengthFunction textsplitter.LenFunction) *Splitter {
	s.Splitter.WithLengthFunction(lengthFunction)
	return s
}
