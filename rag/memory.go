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
	"fmt"
	"strings"

	"github.com/henomis/phero/document"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
	"github.com/henomis/phero/vectorstore"
)

const (
	contextSystemMessagePrefix = "Context retrieved from memory:\n"
)

// AsMemory returns a Memory wrapper around the RAG instance.
// The returned Memory provides a simplified interface for storing and retrieving
// llm.Message objects, making it suitable for conversational contexts where you
// want to store chat history and retrieve relevant past messages.
func (r *RAG) AsMemory() *Memory {
	return &Memory{rag: r}
}

// Memory wraps a RAG instance to provide message-oriented storage and retrieval.
// It formats llm.Message objects as plain text and stores them in the underlying
// vector store, enabling semantic search over conversation history.
type Memory struct {
	rag *RAG
}

var _ memory.Memory = (*Memory)(nil)

// Save stores one or more llm.Message objects in the vector store.
// Messages are formatted as plain text and embedded, allowing later retrieval
// by semantic similarity. Returns an error if ingestion fails.
func (m *Memory) Save(ctx context.Context, messages []llm.Message) error {
	return m.rag.save(ctx, messages)
}

// Clear removes all stored messages from the underlying vector store.
// Returns an error if the clear operation fails.
func (m *Memory) Clear(ctx context.Context) error {
	return m.rag.clear(ctx)
}

// Retrieve searches for messages semantically similar to the given query string.
// It embeds the query, performs a vector similarity search, and returns the matching
// messages ordered by relevance. Points with missing or empty "text" payloads are
// skipped and do not appear in the result.
func (m *Memory) Retrieve(ctx context.Context, query string) ([]llm.Message, error) {
	return m.rag.retrieve(ctx, query)
}

func (s *RAG) save(ctx context.Context, messages []llm.Message) error {
	content := formatSessionContent(messages)
	if err := s.ensureCollection(ctx); err != nil {
		return err
	}
	return s.ingestBatch(ctx, []document.Document{{Content: content}}, 0)
}

func (s *RAG) clear(ctx context.Context) error {
	return s.store.Clear(ctx)
}

func (s *RAG) retrieve(ctx context.Context, query string) ([]llm.Message, error) {
	points, err := s.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	return []llm.Message{pointToContext(points)}, nil
}

// pointToContext converts a slice of ScoredPoint into a single llm.Message
// containing the concatenated text from the points' payloads.
// Points whose payload does not contain a non-empty "text" string are skipped.
func pointToContext(points []vectorstore.ScoredPoint) llm.Message {
	message := llm.Message{
		Role:    llm.ChatMessageRoleSystem,
		Content: contextSystemMessagePrefix,
	}

	for _, p := range points {
		text, ok := p.Payload[contentKey].(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		message.Content += text + "\n"
	}

	return message
}

// formatSessionContent converts a slice of llm.Message into a single string, concatenating the role and content of each message in a readable format.
func formatSessionContent(messages []llm.Message) string {
	var b strings.Builder
	for _, message := range messages {
		if message.Role != llm.ChatMessageRoleAssistant && message.Role != llm.ChatMessageRoleUser {
			continue
		}
		msg := strings.TrimSpace(message.Content)
		if msg == "" {
			continue
		}

		fmt.Fprintf(&b, "%s: %s\n", message.Role, msg)
	}

	return b.String()
}
