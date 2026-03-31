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
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/henomis/phero/document"
	"github.com/henomis/phero/embedding"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/textsplitter"
	"github.com/henomis/phero/vectorstore"
)

const (
	contentKey               = "text"
	defaultTopK              = 4
	defaultEmbedderBatchSize = 200
)

// Option configures a RAG instance.
type Option func(*RAG)

// RAG glues an embedding.Embedder with a concrete Store implementation,
// enabling simple text ingestion and query-by-text.
//
// Ingested texts are stored as points with the original text in payload["text"].
type RAG struct {
	store    vectorstore.Store
	embedder embedding.Embedder

	topk              uint64
	embedderBatchSize int

	ensureMu   sync.Mutex
	ensureDone bool
}

// New constructs a glue component that embeds texts and persists
// them into the provided Store.
func New(store vectorstore.Store, embedder embedding.Embedder, options ...Option) (*RAG, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if embedder == nil {
		return nil, ErrNilEmbedder
	}
	rag := &RAG{store: store, embedder: embedder, topk: defaultTopK, embedderBatchSize: defaultEmbedderBatchSize}

	for _, option := range options {
		option(rag)
	}

	return rag, nil
}

// WithTopK sets the default number of results to return for queries and tool calls.
func WithTopK(topk uint64) Option {
	return func(r *RAG) {
		r.topk = topk
	}
}

// WithBatchSize sets the batch size used for embedding texts during ingestion.
func WithBatchSize(batchSize int) Option {
	return func(r *RAG) {
		if batchSize <= 0 {
			r.embedderBatchSize = defaultEmbedderBatchSize
		} else {
			r.embedderBatchSize = batchSize
		}
	}
}

// ensureCollection calls EnsureCollection on the backing store exactly once
// per RAG instance. A successful call is permanently cached; a failed call
// is not cached so the next caller will retry.
func (s *RAG) ensureCollection(ctx context.Context) error {
	s.ensureMu.Lock()
	defer s.ensureMu.Unlock()

	if s.ensureDone {
		return nil
	}

	if err := s.store.EnsureCollection(ctx); err != nil {
		return &EnsureCollectionError{Cause: err}
	}

	s.ensureDone = true
	return nil
}

// ingestBatch embeds documents and upserts the resulting points into the store.
// offset is the 0-based index of the first document within the overall chunk stream,
// used to populate IngestError fields for diagnostics.
// Each vectorstore Point payload contains the document's Content under the "text"
// key plus all entries from the document's Metadata map.
func (s *RAG) ingestBatch(ctx context.Context, docs []document.Document, offset int) error {
	texts := make([]string, len(docs))
	for i, d := range docs {
		texts[i] = d.Content
	}

	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return &IngestError{Op: "embed", BatchStart: offset, BatchEnd: offset + len(docs), Cause: err}
	}
	if len(vectors) != len(docs) {
		return &EmbedderVectorCountMismatchError{Got: len(vectors), Want: len(docs)}
	}

	points := make([]vectorstore.Point, 0, len(docs))
	for i, d := range docs {
		payload := make(map[string]any, len(d.Metadata)+1)
		for k, v := range d.Metadata {
			payload[k] = v
		}
		payload[contentKey] = d.Content
		points = append(points, vectorstore.Point{
			ID:      uuid.New().String(),
			Vector:  vectors[i],
			Payload: payload,
		})
	}

	if err := s.store.Upsert(ctx, points); err != nil {
		return &IngestError{Op: "upsert", BatchStart: offset, BatchEnd: offset + len(docs), Cause: err}
	}
	return nil
}

// Ingest splits the source configured in the Splitter and ingests the
// resulting chunks. The file path is bound to the Splitter at construction
// time, not passed here.
//
// Chunks are embedded and upserted in rolling batches as they arrive from the
// iterator: at most embedderBatchSize chunks are held in memory at any point.
//
// Returns ErrNoSplitter if no Splitter was provided to New.
// Any error yielded by the splitter iterator is returned immediately, aborting
// ingestion.
func (s *RAG) Ingest(ctx context.Context, splitter textsplitter.Splitter) error {
	if splitter == nil {
		return ErrNoSplitter
	}

	if err := s.ensureCollection(ctx); err != nil {
		return err
	}

	batchSize := s.embedderBatchSize
	if batchSize <= 0 {
		batchSize = defaultEmbedderBatchSize
	}

	batch := make([]document.Document, 0, batchSize)
	offset := 0

	for doc, err := range splitter.Split(ctx) {
		if err != nil {
			return err
		}
		batch = append(batch, doc)
		if len(batch) >= batchSize {
			n := len(batch)
			if err := s.ingestBatch(ctx, batch, offset); err != nil {
				return err
			}
			offset += n
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		return s.ingestBatch(ctx, batch, offset)
	}
	return nil
}

// Query embeds the provided query text and performs a similarity search.
func (s *RAG) Query(ctx context.Context, queryText string) ([]vectorstore.ScoredPoint, error) {
	if strings.TrimSpace(queryText) == "" {
		return nil, ErrEmptyQueryText
	}
	if err := s.ensureCollection(ctx); err != nil {
		return nil, err
	}

	vectors, err := s.embedder.Embed(ctx, []string{queryText})
	if err != nil {
		return nil, &QueryError{Op: "embed", Cause: err}
	}
	if len(vectors) != 1 {
		return nil, &EmbedderVectorCountMismatchError{Got: len(vectors), Want: 1, SingleQuery: true}
	}

	results, err := s.store.Query(ctx, vectors[0], s.topk)
	if err != nil {
		return nil, &QueryError{Op: "store query", Cause: err}
	}
	return results, nil
}

// AsTool exposes this RAG instance as an llm.FunctionTool.
//
// The tool performs similarity search over previously-ingested texts.
// Tool arguments schema:
//
//	{"query": "..."}
func (s *RAG) AsTool(toolName, toolDescription string) (*llm.Tool, error) {
	type ToolInput struct {
		Query string `json:"query" jsonschema:"description=Query text to search for."`
	}

	type Result struct {
		// ID    string  `json:"id" jsonschema:"description=Point identifier."`
		// Score float32 `json:"score" jsonschema:"description=Similarity score (higher is more similar)."`
		Text string `json:"text" jsonschema:"description=Original text stored in the knowledge base."`
	}

	type ToolOutput struct {
		Results []Result `json:"results" jsonschema:"description=Top matches sorted by similarity."`
	}

	handler := func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		if input == nil {
			input = &ToolInput{}
		}

		points, err := s.Query(ctx, input.Query)
		if err != nil {
			return nil, err
		}

		out := make([]Result, 0, len(points))
		for _, p := range points {
			text, _ := p.Payload[contentKey].(string)
			out = append(out, Result{
				// ID:    p.ID,
				// Score: p.Score,
				Text: text,
			})
		}

		return &ToolOutput{Results: out}, nil
	}

	return llm.NewTool(
		toolName,
		toolDescription,
		handler,
	)
}
