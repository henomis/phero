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
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henomis/phero/vectorstore"
	vspsql "github.com/henomis/phero/vectorstore/psql"
	vsqdrant "github.com/henomis/phero/vectorstore/qdrant"
)

// ---- Qdrant ----------------------------------------------------------------

// TestVectorStoreQdrant_UpsertQuery exercises Qdrant upsert + query.
// Requires a running Qdrant instance (skipped otherwise).
func TestVectorStoreQdrant_UpsertQuery(t *testing.T) {
	qc := requireQdrant(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	collection := "e2e-test-" + uuid.New().String()

	const vectorSize = 4

	store, err := vsqdrant.New(qc, collection, vsqdrant.WithVectorSize(vectorSize))
	if err != nil {
		t.Fatalf("vsqdrant.New: %v", err)
	}

	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := store.Clear(cleanCtx); err != nil {
			t.Logf("cleanup Clear: %v", err)
		}
	})

	idAlpha := uuid.New().String()
	idBeta := uuid.New().String()
	idGamma := uuid.New().String()

	points := []vectorstore.Point{
		{ID: idAlpha, Vector: []float32{1, 0, 0, 0}, Payload: map[string]any{"label": "alpha"}},
		{ID: idBeta, Vector: []float32{0, 1, 0, 0}, Payload: map[string]any{"label": "beta"}},
		{ID: idGamma, Vector: []float32{0, 0, 1, 0}, Payload: map[string]any{"label": "gamma"}},
	}

	if err := store.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Query closest to idAlpha.
	results, err := store.Query(ctx, []float32{1, 0, 0, 0}, 2)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	t.Logf("Top result: id=%s score=%.4f", results[0].ID, results[0].Score)

	if results[0].ID != idAlpha {
		t.Errorf("expected top result id=%q (alpha), got %q", idAlpha, results[0].ID)
	}
}

// TestVectorStoreQdrant_Delete verifies that deleted points no longer appear.
func TestVectorStoreQdrant_Delete(t *testing.T) {
	qc := requireQdrant(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	collection := "e2e-del-" + uuid.New().String()

	const vectorSize = 4

	store, err := vsqdrant.New(qc, collection, vsqdrant.WithVectorSize(vectorSize))
	if err != nil {
		t.Fatalf("vsqdrant.New: %v", err)
	}

	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_ = store.Clear(cleanCtx)
	})

	idX1 := uuid.New().String()
	idX2 := uuid.New().String()

	points := []vectorstore.Point{
		{ID: idX1, Vector: []float32{1, 0, 0, 0}, Payload: map[string]any{}},
		{ID: idX2, Vector: []float32{0, 1, 0, 0}, Payload: map[string]any{}},
	}

	if err := store.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := store.Delete(ctx, []string{idX1}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := store.Query(ctx, []float32{1, 0, 0, 0}, 5)
	if err != nil {
		t.Fatalf("Query after Delete: %v", err)
	}

	for _, r := range results {
		if r.ID == idX1 {
			t.Error("deleted point still appears in query results")
		}
	}
}

// TestVectorStoreQdrant_Clear verifies that clearing the collection removes all points.
func TestVectorStoreQdrant_Clear(t *testing.T) {
	qc := requireQdrant(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	collection := "e2e-clr-" + uuid.New().String()

	const vectorSize = 4

	store, err := vsqdrant.New(qc, collection, vsqdrant.WithVectorSize(vectorSize))
	if err != nil {
		t.Fatalf("vsqdrant.New: %v", err)
	}

	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_ = store.Clear(cleanCtx)
	})

	points := []vectorstore.Point{
		{ID: uuid.New().String(), Vector: []float32{1, 0, 0, 0}, Payload: map[string]any{}},
		{ID: uuid.New().String(), Vector: []float32{0, 1, 0, 0}, Payload: map[string]any{}},
	}

	if err := store.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := store.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	results, err := store.Query(ctx, []float32{1, 0, 0, 0}, 5)
	if err != nil {
		t.Fatalf("Query after Clear: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results after Clear, got %d", len(results))
	}
}

// ---- PostgreSQL (pgvector) -------------------------------------------------

// TestVectorStorePSQL_UpsertQuery exercises PostgreSQL+pgvector upsert + query.
// Requires a running PostgreSQL with pgvector (skipped otherwise).
func TestVectorStorePSQL_UpsertQuery(t *testing.T) {
	db := requirePostgres(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	collection := "e2e_" + uuid.New().String()[:8]

	const vectorSize = 4

	store, err := vspsql.New(db, collection,
		vspsql.WithVectorSize(vectorSize),
		vspsql.WithEnsureExtension(true),
	)
	if err != nil {
		t.Fatalf("vspsql.New: %v", err)
	}

	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_ = store.Clear(cleanCtx)
	})

	points := []vectorstore.Point{
		{ID: "aa", Vector: []float32{1, 0, 0, 0}, Payload: map[string]any{"tag": "first"}},
		{ID: "bb", Vector: []float32{0, 1, 0, 0}, Payload: map[string]any{"tag": "second"}},
		{ID: "cc", Vector: []float32{0, 0, 1, 0}, Payload: map[string]any{"tag": "third"}},
	}

	if err := store.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	results, err := store.Query(ctx, []float32{1, 0, 0, 0}, 2)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	t.Logf("Top result: id=%s score=%.4f", results[0].ID, results[0].Score)

	if results[0].ID != "aa" {
		t.Errorf("expected top result id='aa', got %q", results[0].ID)
	}
}

// TestVectorStorePSQL_Delete verifies that deleted points are gone.
func TestVectorStorePSQL_Delete(t *testing.T) {
	db := requirePostgres(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	collection := "e2e_del_" + uuid.New().String()[:8]

	const vectorSize = 4

	store, err := vspsql.New(db, collection,
		vspsql.WithVectorSize(vectorSize),
		vspsql.WithEnsureExtension(true),
	)
	if err != nil {
		t.Fatalf("vspsql.New: %v", err)
	}

	if err := store.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}

	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		_ = store.Clear(cleanCtx)
	})

	points := []vectorstore.Point{
		{ID: "d1", Vector: []float32{1, 0, 0, 0}, Payload: map[string]any{}},
		{ID: "d2", Vector: []float32{0, 1, 0, 0}, Payload: map[string]any{}},
	}

	if err := store.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := store.Delete(ctx, []string{"d1"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	results, err := store.Query(ctx, []float32{1, 0, 0, 0}, 5)
	if err != nil {
		t.Fatalf("Query after Delete: %v", err)
	}

	for _, r := range results {
		if r.ID == "d1" {
			t.Error("deleted point 'd1' still appears in results")
		}
	}
}
