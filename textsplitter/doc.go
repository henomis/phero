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

// Package textsplitter provides utilities to split text into size-bounded,
// optionally-overlapping chunks.
//
// This package currently includes a recursive character-based splitter that
// chooses a separator from an ordered list (by default: "\n\n", "\n", " ", "")
// and recursively splits long segments using progressively smaller separators.
// Adjacent segments are then merged into chunks according to the configured
// chunk size and overlap.
//
// Chunk size and overlap are measured using a configurable length function.
// By default, length is measured in bytes; callers can supply a rune-counting
// implementation when working with Unicode text.
package textsplitter
