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
	"testing"

	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/tracetest"
	"github.com/henomis/phero/v2/vectorstore"
)

type hitStore struct{ stubStore }

func (*hitStore) Query(context.Context, vectorstore.Vector, uint64, ...vectorstore.QueryOption) ([]vectorstore.ScoredPoint, error) {
	return []vectorstore.ScoredPoint{{ID: "p1", Score: 0.9, Payload: map[string]any{contentKey: "doc", "src": "a.md"}}}, nil
}

func TestQuery_RecordsRetrievalAndEmbeddingSpans(t *testing.T) {
	rec := tracetest.NewRecorder()
	ctx := trace.WithProvider(context.Background(), trace.NewProvider(trace.WithProcessor(rec)))

	r, err := New(&hitStore{}, stubEmbedder{}, WithTopK(3))
	if err != nil {
		t.Fatal(err)
	}

	if _, err = r.Query(ctx, "what?"); err != nil {
		t.Fatal(err)
	}

	retrievals := rec.ByKind(trace.KindRetrieval)
	if len(retrievals) != 1 {
		t.Fatalf("expected 1 retrieval span, got %d", len(retrievals))
	}

	p, ok := retrievals[0].Payload.(*trace.Retrieval)
	if !ok || p.Query != "what?" || p.TopK != 3 || len(p.Hits) != 1 || p.Hits[0].Content != "doc" || p.Hits[0].Metadata["src"] != "a.md" {
		t.Fatalf("unexpected retrieval payload %+v", retrievals[0].Payload)
	}

	emb := rec.ByKind(trace.KindEmbedding)
	if len(emb) != 1 || emb[0].Parent.SpanID != retrievals[0].Context.SpanID {
		t.Fatalf("expected the embedding span under the retrieval span, got %+v", emb)
	}
}
