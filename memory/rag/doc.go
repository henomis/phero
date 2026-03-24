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

// Package rag provides a memory.Memory implementation backed by the project's
// retrieval-augmented generation (RAG) store.
//
// Import path note:
//   - This package lives at "github.com/henomis/phero/memory/rag".
//   - It wraps types from the top-level "github.com/henomis/phero/rag".
//
// The adapter is useful when you want an agent's "memory" to be semantic: saving
// and retrieving messages via similarity search instead of a simple FIFO buffer.
//
// Usage:
//
//	r, err := rag.New(store, embedder)
//	if err != nil {
//		// handle error
//	}
//	mem := memrag.New(r)
//
//	ag, err := agent.New(client, "name", "description")
//	if err != nil {
//		// handle error
//	}
//	ag.SetMemory(mem)
package rag
