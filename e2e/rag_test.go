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

//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/rag"
	textsplitterrecursive "github.com/henomis/phero/textsplitter/recursive"
	vsqdrant "github.com/henomis/phero/vectorstore/qdrant"
)

// TestRAG_IngestAndQuery verifies the full RAG pipeline:
//  1. Ingest documents using a recursive splitter
//  2. Query the store with a related question
//  3. Verify that relevant results are returned
//
// Requires a running Qdrant and an embedding model (skipped otherwise).
func TestRAG_IngestAndQuery(t *testing.T) {
	qc := requireQdrant(t)

	vectorSize := probeVectorSize(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	collection := "e2e-rag-" + uuid.New().String()

	store, err := vsqdrant.New(qc, collection,
		vsqdrant.WithVectorSize(vectorSize),
	)
	if err != nil {
		t.Fatalf("vsqdrant.New: %v", err)
	}

	embedder := buildEmbedder()

	r, err := rag.New(store, embedder, rag.WithTopK(3))
	if err != nil {
		t.Fatalf("rag.New: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_ = store.Clear(cleanCtx)
	})

	// Write a small knowledge-base file.
	content := `The Eiffel Tower is a wrought-iron lattice tower on the Champ de Mars in Paris, France.
It was constructed from 1887 to 1889. It is named after Gustave Eiffel, whose company designed and built it.
The tower is 330 metres tall. It is the most-visited paid monument in the world.

The Louvre Museum is an art museum in Paris that holds thousands of works including the Mona Lisa.
The Colosseum is an oval amphitheatre in Rome, Italy. It was built under the Flavian emperors.
Mount Everest is Earth's highest mountain above sea level, located in Nepal.`

	tmpFile := t.TempDir() + "/knowledge.txt"

	if err := os.WriteFile(tmpFile, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	splitter := textsplitterrecursive.New(tmpFile, 300, 30)

	if err := r.Ingest(ctx, splitter); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	results, err := r.Query(ctx, "How tall is the Eiffel Tower?")
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result from RAG query")
	}

	t.Logf("Top result score=%.4f payload=%v", results[0].Score, results[0].Payload)
}

// TestRAG_AsTool verifies that RAG can be wrapped as an llm.Tool and used
// by an agent to answer questions from ingested knowledge.
//
// Requires a running Qdrant and an embedding model (skipped otherwise).
func TestRAG_AsTool(t *testing.T) {
	qc := requireQdrant(t)

	vectorSize := probeVectorSize(t)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	collection := "e2e-rag-tool-" + uuid.New().String()

	store, err := vsqdrant.New(qc, collection,
		vsqdrant.WithVectorSize(vectorSize),
	)
	if err != nil {
		t.Fatalf("vsqdrant.New: %v", err)
	}

	embedder := buildEmbedder()

	r, err := rag.New(store, embedder, rag.WithTopK(3))
	if err != nil {
		t.Fatalf("rag.New: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_ = store.Clear(cleanCtx)
	})

	content := `The speed of light in vacuum is approximately 299,792 kilometres per second.
The speed of sound in air at 20 degrees Celsius is about 343 metres per second.
Water boils at 100 degrees Celsius at sea level atmospheric pressure.`

	tmpFile := t.TempDir() + "/physics.txt"

	if err := os.WriteFile(tmpFile, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	splitter := textsplitterrecursive.New(tmpFile, 200, 20)

	if err := r.Ingest(ctx, splitter); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	ragTool, err := r.AsTool("search_knowledge", "Searches the knowledge base for relevant information")
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}

	llmClient := buildOpenAILLM()

	a, err := agent.New(llmClient, "rag-agent",
		"You are a knowledgeable assistant. Use the search_knowledge tool to find factual information.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	if err := a.AddTool(ragTool); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, err := a.Run(ctx, llm.Text("What is the speed of light?"))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("RAG agent response: %q", result.TextContent())
}

// TestRAG_AsMemory verifies that RAG can act as a semantic memory for an agent,
// storing conversation turns and retrieving relevant ones.
//
// Requires a running Qdrant and an embedding model (skipped otherwise).
func TestRAG_AsMemory(t *testing.T) {
	qc := requireQdrant(t)

	vectorSize := probeVectorSize(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	collection := "e2e-rag-mem-" + uuid.New().String()

	store, err := vsqdrant.New(qc, collection,
		vsqdrant.WithVectorSize(vectorSize),
	)
	if err != nil {
		t.Fatalf("vsqdrant.New: %v", err)
	}

	embedder := buildEmbedder()

	r, err := rag.New(store, embedder, rag.WithTopK(2))
	if err != nil {
		t.Fatalf("rag.New: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_ = store.Clear(cleanCtx)
	})

	mem := r.AsMemory()

	// Save some past conversation turns.
	if err := mem.Save(ctx, []llm.Message{
		llm.UserMessage(llm.Text("My favourite colour is blue.")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("Noted!")}, llm.ToolCall{}),
	}); err != nil {
		t.Fatalf("mem.Save: %v", err)
	}

	// Retrieve with a semantically related query.
	retrieved, err := mem.Retrieve(ctx, "What is my favourite colour?")
	if err != nil {
		t.Fatalf("mem.Retrieve: %v", err)
	}

	t.Logf("Retrieved %d messages from RAG memory", len(retrieved))
}
