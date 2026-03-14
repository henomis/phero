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

// Package embedding defines a small, provider-agnostic interface for generating
// vector embeddings from text.
//
// The core abstraction is the Embedder interface, which turns a slice of input
// texts into a slice of embedding vectors while preserving input order.
//
// This package also defines common types used across implementations:
// - Vector: an embedding vector (typically []float32)
// - Model: a provider-specific model identifier string
//
// Provider implementations live in subpackages (for example, embedding/openai).
package embedding
