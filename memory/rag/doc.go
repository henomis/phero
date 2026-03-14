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
