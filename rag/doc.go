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

// Package rag provides a small retrieval-augmented generation (RAG) helper.
//
// The rag.RAG type glues together an embedding.Embedder and a vectorstore.Store.
// It supports:
// - Ingest: embed and store a list of texts as vectors (payload includes the original text)
// - Query: embed a query string and return the most similar stored texts
// - AsTool: expose Query as an llm.FunctionTool suitable for agent use
// - AsMemory: wrap the RAG as a Memory for storing and retrieving llm.Message objects
//
// The Memory type provides a message-oriented interface for conversational contexts:
// - Save: store llm.Message objects (serialized as JSON vectors)
// - Retrieve: search for semantically similar messages
// - Clear: remove all stored messages
//
// This package does not implement chunking/splitting; pair it with a splitter
// implementation (for example, textsplitter/recursive) to turn documents into
// chunks before ingestion.
package rag
