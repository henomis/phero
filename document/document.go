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

package document

// Document is a text chunk produced by a text splitter, paired with
// metadata that describes its origin.
//
// Metadata keys are unstructured; splitters populate the following
// well-known keys automatically:
//
//   - "source"       — the file path the chunk was read from
//   - "chunk_index"  — 0-based position of the chunk in the split sequence
//   - "start_offset" — byte offset of the chunk's first byte in the source file (-1 if unknown)
//   - "end_offset"   — byte offset one past the chunk's last byte in the source file (-1 if unknown)
type Document struct {
	// Content is the text content of the chunk.
	Content string

	// Metadata holds arbitrary key/value pairs that describe the chunk's
	// provenance and position within its source.
	Metadata map[string]any
}
