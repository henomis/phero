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

package openai

import "fmt"

// ResponseIndexOutOfRangeError is returned when an embeddings response contains
// an item index that does not fit in the expected output slice.
type ResponseIndexOutOfRangeError struct {
	Index int
	Len   int
}

func (e *ResponseIndexOutOfRangeError) Error() string {
	return fmt.Sprintf("embedding response index out of range: %d (len=%d)", e.Index, e.Len)
}

// MissingEmbeddingError is returned when an embeddings response does not contain
// an embedding for a requested input index.
type MissingEmbeddingError struct {
	Index int
}

func (e *MissingEmbeddingError) Error() string {
	return fmt.Sprintf("missing embedding for input index %d", e.Index)
}
