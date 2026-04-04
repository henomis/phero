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

package qdrant_test

import (
	"context"
	"errors"
	"testing"

	qdrantapi "github.com/qdrant/go-client/qdrant"

	"github.com/henomis/phero/vectorstore"
	"github.com/henomis/phero/vectorstore/qdrant"
)

// fakeClient returns a *qdrantapi.Client configured to point at a
// non-listening address. The client is only used to satisfy the New() argument;
// validation errors tested here are returned before any network I/O.
func fakeClient(t *testing.T) *qdrantapi.Client {
	t.Helper()
	c, err := qdrantapi.NewClient(&qdrantapi.Config{
		Host: "127.0.0.1",
		Port: 19999, // nothing listens here
	})
	if err != nil {
		t.Fatalf("qdrantapi.NewClient: %v", err)
	}
	return c
}

// -- constructor tests -------------------------------------------------------

func TestNew_NilClientReturnsError(t *testing.T) {
	_, err := qdrant.New(nil, "my_collection", qdrant.WithVectorSize(4))
	if !errors.Is(err, qdrant.ErrNilClient) {
		t.Fatalf("expected ErrNilClient, got %v", err)
	}
}

func TestNew_EmptyCollectionReturnsError(t *testing.T) {
	_, err := qdrant.New(fakeClient(t), "", qdrant.WithVectorSize(4))
	if !errors.Is(err, qdrant.ErrEmptyCollection) {
		t.Fatalf("expected ErrEmptyCollection, got %v", err)
	}
}

func TestNew_ZeroVectorSizeReturnsError(t *testing.T) {
	_, err := qdrant.New(fakeClient(t), "col")
	if !errors.Is(err, qdrant.ErrInvalidVectorSize) {
		t.Fatalf("expected ErrInvalidVectorSize, got %v", err)
	}
}

func TestNew_Valid(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Store")
	}
}

// -- Upsert validation tests -------------------------------------------------

func TestUpsert_EmptyPointsReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = s.Upsert(context.Background(), nil)
	if !errors.Is(err, vectorstore.ErrEmptyPoints) {
		t.Fatalf("expected ErrEmptyPoints, got %v", err)
	}
}

func TestUpsert_EmptyPointIDReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	points := []vectorstore.Point{
		{ID: "", Vector: []float32{1, 2, 3, 4}},
	}
	err = s.Upsert(context.Background(), points)
	if !errors.Is(err, qdrant.ErrPointIDRequired) {
		t.Fatalf("expected ErrPointIDRequired, got %v", err)
	}
}

func TestUpsert_EmptyVectorReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	points := []vectorstore.Point{
		{ID: "p1", Vector: nil},
	}
	err = s.Upsert(context.Background(), points)

	var emptyVecErr *qdrant.EmptyVectorError
	if !errors.As(err, &emptyVecErr) {
		t.Fatalf("expected EmptyVectorError, got %v", err)
	}
	if emptyVecErr.PointID != "p1" {
		t.Fatalf("expected PointID %q, got %q", "p1", emptyVecErr.PointID)
	}
}

func TestUpsert_VectorSizeMismatchReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	points := []vectorstore.Point{
		{ID: "p1", Vector: []float32{1, 2, 3}}, // 3 instead of 4
	}
	err = s.Upsert(context.Background(), points)

	var mismatch *qdrant.VectorSizeMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected VectorSizeMismatchError, got %v", err)
	}
	if mismatch.Expected != 4 || mismatch.Got != 3 {
		t.Fatalf("mismatch fields: expected=%d got=%d", mismatch.Expected, mismatch.Got)
	}
}

// -- Query validation tests --------------------------------------------------

func TestQuery_EmptyQueryReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Query(context.Background(), nil, 5)
	if !errors.Is(err, vectorstore.ErrEmptyQuery) {
		t.Fatalf("expected ErrEmptyQuery, got %v", err)
	}
}

func TestQuery_ZeroLimitReturnsEmpty(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := s.Query(context.Background(), []float32{1, 2, 3, 4}, 0)
	if err != nil {
		t.Fatalf("Query: unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty results, got %d", len(results))
	}
}

func TestQuery_VectorSizeMismatchReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Query(context.Background(), []float32{1, 2}, 5) // 2 instead of 4
	var mismatch *qdrant.VectorSizeMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected VectorSizeMismatchError, got %v", err)
	}
}

// -- Delete validation tests -------------------------------------------------

func TestDelete_EmptyIDsReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = s.Delete(context.Background(), nil)
	if !errors.Is(err, vectorstore.ErrEmptyIDs) {
		t.Fatalf("expected ErrEmptyIDs, got %v", err)
	}
}

func TestDelete_AllBlankIDsReturnsError(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col", qdrant.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = s.Delete(context.Background(), []string{"", ""})
	if !errors.Is(err, vectorstore.ErrEmptyIDs) {
		t.Fatalf("expected ErrEmptyIDs, got %v", err)
	}
}

// -- option tests ------------------------------------------------------------

func TestWithBatchSize_Option(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col",
		qdrant.WithVectorSize(4),
		qdrant.WithBatchSize(10),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Store")
	}
}

func TestWithDistance_Option(t *testing.T) {
	s, err := qdrant.New(fakeClient(t), "col",
		qdrant.WithVectorSize(4),
		qdrant.WithDistance(qdrant.DistanceEuclid),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Store")
	}
}
