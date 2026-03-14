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

package embedding

import (
	"context"
	"errors"
)

// ErrEmptyInput is returned when an embedding call receives no input texts.
var ErrEmptyInput = errors.New("empty input")

// Vector is an embedding vector.
//
// Most OpenAI-compatible embedding endpoints return float32 vectors.
type Vector = []float32

// Model identifies an embedding model name.
//
// Examples:
// - OpenAI: "text-embedding-3-small"
// - Ollama: "nomic-embed-text".
type Model string

// Embedder generates vector embeddings for input texts.
//
// Implementations should preserve the ordering of inputs.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([]Vector, error)
}
