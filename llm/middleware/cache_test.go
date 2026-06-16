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

package middleware_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/henomis/phero/embedding"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/middleware"
	"github.com/henomis/phero/vectorstore"
)

// mapEmbedder maps each input string to a vector via fn.
type mapEmbedder struct {
	fn func(string) embedding.Vector
}

func (m mapEmbedder) Embed(_ context.Context, texts []string) ([]embedding.Vector, error) {
	out := make([]embedding.Vector, len(texts))
	for i, t := range texts {
		out[i] = m.fn(t)
	}
	return out, nil
}

// memStore is an in-memory vectorstore.Store that ranks by cosine similarity.
type memStore struct {
	points      []vectorstore.Point
	ensureCalls int
}

func (s *memStore) EnsureCollection(context.Context) error {
	s.ensureCalls++
	return nil
}

func (s *memStore) Upsert(_ context.Context, points []vectorstore.Point) error {
	s.points = append(s.points, points...)
	return nil
}

func (s *memStore) Query(_ context.Context, query vectorstore.Vector, limit uint64, _ ...vectorstore.QueryOption) ([]vectorstore.ScoredPoint, error) {
	var best *vectorstore.ScoredPoint
	for i := range s.points {
		score := cosine(query, s.points[i].Vector)
		if best == nil || score > best.Score {
			best = &vectorstore.ScoredPoint{ID: s.points[i].ID, Score: score, Payload: s.points[i].Payload}
		}
	}
	if best == nil || limit == 0 {
		return nil, nil
	}
	return []vectorstore.ScoredPoint{*best}, nil
}

func (s *memStore) Delete(context.Context, []string) error { return nil }
func (s *memStore) Clear(context.Context) error            { return nil }
func (s *memStore) Count(context.Context) (uint64, error)  { return uint64(len(s.points)), nil }

func cosine(a, b vectorstore.Vector) float32 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

// countingLLM returns a fixed result and counts how many times it is called.
type countingLLM struct {
	calls int
	text  string
}

func (c *countingLLM) Execute(context.Context, []llm.Message, []*llm.Tool) (*llm.Result, error) {
	c.calls++
	msg := llm.AssistantMessage([]llm.ContentPart{llm.Text(c.text)})
	return &llm.Result{
		Message: &msg,
		Usage:   &llm.Usage{InputTokens: 10, OutputTokens: 5},
		Model:   "test-model",
	}, nil
}

func TestSemanticCache_HitAvoidsSecondCall(t *testing.T) {
	// Same text -> identical vector -> cosine 1.0 -> hit.
	emb := mapEmbedder{fn: func(s string) embedding.Vector {
		if s == "" {
			return embedding.Vector{0, 0, 1}
		}
		return embedding.Vector{1, 0, 0}
	}}
	store := &memStore{}
	inner := &countingLLM{text: "cached answer"}

	mw, err := middleware.NewSemanticCache(emb, store)
	if err != nil {
		t.Fatalf("NewSemanticCache: %v", err)
	}
	client := llm.Use(inner, mw)

	msgs := []llm.Message{llm.UserMessage(llm.Text("hello"))}

	first, err := client.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected 1 inner call after miss, got %d", inner.calls)
	}
	if first.Usage.InputTokens != 10 {
		t.Fatalf("expected original usage on miss, got %+v", first.Usage)
	}

	second, err := client.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("expected inner NOT called on hit, got %d calls", inner.calls)
	}
	if got := second.Message.TextContent(); got != "cached answer" {
		t.Fatalf("unexpected cached content: %q", got)
	}
	if second.Usage == nil || second.Usage.InputTokens != 0 || second.Usage.OutputTokens != 0 {
		t.Fatalf("expected zeroed usage on hit, got %+v", second.Usage)
	}
	if store.ensureCalls != 1 {
		t.Fatalf("expected EnsureCollection called once, got %d", store.ensureCalls)
	}
}

func TestSemanticCache_BelowThresholdMisses(t *testing.T) {
	// Orthogonal vectors -> cosine 0 -> never a hit.
	calls := 0
	emb := mapEmbedder{fn: func(string) embedding.Vector {
		calls++
		switch calls {
		case 1:
			return embedding.Vector{1, 0, 0}
		default:
			return embedding.Vector{0, 1, 0}
		}
	}}
	store := &memStore{}
	inner := &countingLLM{text: "answer"}

	mw, err := middleware.NewSemanticCache(emb, store, middleware.WithSimilarityThreshold(0.9))
	if err != nil {
		t.Fatalf("NewSemanticCache: %v", err)
	}
	client := llm.Use(inner, mw)

	_, _ = client.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("a"))}, nil)
	_, _ = client.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("b"))}, nil)

	if inner.calls != 2 {
		t.Fatalf("expected 2 inner calls (both misses), got %d", inner.calls)
	}
}

func TestSemanticCache_SkipsWhenToolsPresent(t *testing.T) {
	emb := mapEmbedder{fn: func(string) embedding.Vector { return embedding.Vector{1, 0, 0} }}
	store := &memStore{}
	inner := &countingLLM{text: "answer"}

	mw, err := middleware.NewSemanticCache(emb, store)
	if err != nil {
		t.Fatalf("NewSemanticCache: %v", err)
	}
	client := llm.Use(inner, mw)

	tool, err := llm.NewTool("noop", "does nothing", func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}
	_, _ = client.Execute(context.Background(), msgs, []*llm.Tool{tool})
	_, _ = client.Execute(context.Background(), msgs, []*llm.Tool{tool})

	if inner.calls != 2 {
		t.Fatalf("expected tools to bypass cache (2 calls), got %d", inner.calls)
	}
	if len(store.points) != 0 {
		t.Fatalf("expected nothing cached when tools present, got %d points", len(store.points))
	}
}

func TestNewSemanticCache_Validation(t *testing.T) {
	emb := mapEmbedder{fn: func(string) embedding.Vector { return embedding.Vector{1} }}
	store := &memStore{}

	if _, err := middleware.NewSemanticCache(nil, store); !errors.Is(err, middleware.ErrNilEmbedder) {
		t.Fatalf("expected ErrNilEmbedder, got %v", err)
	}
	if _, err := middleware.NewSemanticCache(emb, nil); !errors.Is(err, middleware.ErrNilStore) {
		t.Fatalf("expected ErrNilStore, got %v", err)
	}
	if _, err := middleware.NewSemanticCache(emb, store, middleware.WithSimilarityThreshold(1.5)); !errors.Is(err, middleware.ErrInvalidThreshold) {
		t.Fatalf("expected ErrInvalidThreshold, got %v", err)
	}
}
