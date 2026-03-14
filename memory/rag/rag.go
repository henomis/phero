package rag

import (
	"context"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
	"github.com/henomis/phero/rag"
)

var _ memory.Memory = (*Memory)(nil)

// Memory is a memory.Memory implementation backed by a RAG store.
//
// It is an adapter around rag.Memory (from the top-level rag package) so it can
// be plugged into agents expecting the memory.Memory interface.
type Memory struct {
	ragMemory *rag.Memory
}

// New creates a new Memory backed by the provided RAG instance.
//
// The given RAG is converted to a message-oriented rag.Memory via rag.AsMemory.
func New(r *rag.RAG) *Memory {
	return &Memory{ragMemory: r.AsMemory()}
}

// Save stores the provided messages in the underlying RAG memory.
func (m *Memory) Save(ctx context.Context, messages []llm.Message) error {
	return m.ragMemory.Save(ctx, messages)
}

// Clear removes all stored messages from the underlying RAG memory.
func (m *Memory) Clear(ctx context.Context) error {
	return m.ragMemory.Clear(ctx)
}

// Retrieve searches the underlying RAG memory for messages relevant to query.
func (m *Memory) Retrieve(ctx context.Context, query string) ([]llm.Message, error) {
	return m.ragMemory.Retrieve(ctx, query)
}
