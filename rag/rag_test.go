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
	"errors"
	"iter"
	"sync"
	"testing"

	"github.com/henomis/phero/document"
	"github.com/henomis/phero/embedding"
	"github.com/henomis/phero/vectorstore"
)

type stubEmbedder struct{}

func (stubEmbedder) Embed(_ context.Context, texts []string) ([]embedding.Vector, error) {
	vectors := make([]embedding.Vector, len(texts))
	for i := range texts {
		vectors[i] = embedding.Vector{float32(i + 1)}
	}
	return vectors, nil
}

type stubStore struct {
	mu            sync.Mutex
	ensureCalls   int
	ensureErrs    []error
	ensureStarted chan struct{}
	ensureRelease chan struct{}
}

func (s *stubStore) EnsureCollection(_ context.Context) error {
	s.mu.Lock()
	callIndex := s.ensureCalls
	s.ensureCalls++
	started := s.ensureStarted
	release := s.ensureRelease
	var err error
	if callIndex < len(s.ensureErrs) {
		err = s.ensureErrs[callIndex]
	}
	s.mu.Unlock()

	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if release != nil {
		<-release
	}

	return err
}

func (s *stubStore) Upsert(_ context.Context, _ []vectorstore.Point) error {
	return nil
}

func (s *stubStore) Query(_ context.Context, _ vectorstore.Vector, _ uint64) ([]vectorstore.ScoredPoint, error) {
	return []vectorstore.ScoredPoint{}, nil
}

func (s *stubStore) Delete(_ context.Context, _ []string) error {
	return nil
}

func (s *stubStore) Clear(_ context.Context) error {
	return nil
}

func (s *stubStore) Count(_ context.Context) (uint64, error) {
	return 0, nil
}

func (s *stubStore) EnsureCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureCalls
}

type stubSplitter struct {
	chunks []string
}

func (s *stubSplitter) Split(_ context.Context) iter.Seq2[document.Document, error] {
	return func(yield func(document.Document, error) bool) {
		for _, chunk := range s.chunks {
			if !yield(document.Document{Content: chunk}, nil) {
				return
			}
		}
	}
}

func TestRAGEnsureCollectionCachedAfterSuccess(t *testing.T) {
	store := &stubStore{}
	splitter := &stubSplitter{chunks: []string{"first"}}
	r, err := New(store, stubEmbedder{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := r.Ingest(ctx, splitter); err != nil {
		t.Fatalf("IngestFile() error = %v", err)
	}
	if _, err := r.Query(ctx, "question"); err != nil {
		t.Fatalf("Query() error = %v", err)
	}

	if got := store.EnsureCalls(); got != 1 {
		t.Fatalf("EnsureCollection() calls = %d, want 1", got)
	}
}

func TestRAGEnsureCollectionRetriesAfterFailure(t *testing.T) {
	store := &stubStore{ensureErrs: []error{errors.New("temporary ensure failure"), nil}}
	splitter := &stubSplitter{chunks: []string{"first"}}
	r, err := New(store, stubEmbedder{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	if err := r.Ingest(ctx, splitter); err == nil {
		t.Fatal("IngestFile() error = nil, want transient ensure error")
	}
	if err := r.Ingest(ctx, splitter); err != nil {
		t.Fatalf("second IngestFile() error = %v", err)
	}

	if got := store.EnsureCalls(); got != 2 {
		t.Fatalf("EnsureCollection() calls = %d, want 2", got)
	}
}

func TestRAGEnsureCollectionConcurrentCallersShareSuccess(t *testing.T) {
	store := &stubStore{
		ensureStarted: make(chan struct{}, 1),
		ensureRelease: make(chan struct{}),
	}
	r, err := New(store, stubEmbedder{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	const goroutines = 8
	errCh := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Query(ctx, "shared question")
			errCh <- err
		}()
	}

	<-store.ensureStarted
	close(store.ensureRelease)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
	}

	if got := store.EnsureCalls(); got != 1 {
		t.Fatalf("EnsureCollection() calls = %d, want 1", got)
	}
}

type countingStore struct {
	stubStore
	count       uint64
	upsertCalls int
}

func (s *countingStore) Count(_ context.Context) (uint64, error) {
	return s.count, nil
}

func (s *countingStore) Upsert(_ context.Context, _ []vectorstore.Point) error {
	s.upsertCalls++
	return nil
}

func TestRAG_IngestOnce_SkipsWhenNonEmpty(t *testing.T) {
	store := &countingStore{count: 5}
	splitter := &stubSplitter{chunks: []string{"chunk"}}
	r, err := New(store, stubEmbedder{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := r.IngestOnce(context.Background(), splitter); err != nil {
		t.Fatalf("IngestOnce() error = %v", err)
	}
	if store.upsertCalls != 0 {
		t.Fatalf("Upsert called %d times, want 0 (collection was non-empty)", store.upsertCalls)
	}
}

func TestRAG_IngestOnce_IngestsWhenEmpty(t *testing.T) {
	store := &countingStore{count: 0}
	splitter := &stubSplitter{chunks: []string{"chunk1", "chunk2"}}
	r, err := New(store, stubEmbedder{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := r.IngestOnce(context.Background(), splitter); err != nil {
		t.Fatalf("IngestOnce() error = %v", err)
	}
	if store.upsertCalls == 0 {
		t.Fatal("Upsert not called, want at least one call (collection was empty)")
	}
}
