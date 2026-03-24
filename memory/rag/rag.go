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
