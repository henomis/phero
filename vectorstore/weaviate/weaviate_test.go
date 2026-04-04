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

package weaviate_test

import (
	"context"
	"errors"
	"testing"

	weaviateclient "github.com/weaviate/weaviate-go-client/v4/weaviate"

	"github.com/henomis/phero/vectorstore"
	"github.com/henomis/phero/vectorstore/weaviate"
)

// fakeClient returns a *weaviateclient.Client pointing at a non-listening
// address.  It is only used to satisfy New(), and the validation errors tested
// here are returned before any network I/O.
func fakeClient() *weaviateclient.Client {
	return weaviateclient.New(weaviateclient.Config{
		Host:   "127.0.0.1:19998",
		Scheme: "http",
	})
}

// -- constructor tests -------------------------------------------------------

func TestNew_NilClientReturnsError(t *testing.T) {
	_, err := weaviate.New(nil, "MyCollection")
	if !errors.Is(err, weaviate.ErrNilClient) {
		t.Fatalf("expected ErrNilClient, got %v", err)
	}
}

func TestNew_EmptyClassReturnsError(t *testing.T) {
	_, err := weaviate.New(fakeClient(), "")
	if !errors.Is(err, weaviate.ErrEmptyCollection) {
		t.Fatalf("expected ErrEmptyCollection, got %v", err)
	}
}

func TestNew_Valid(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "TestCollection")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Store")
	}
}

func TestNew_ClassNameCapitalized(t *testing.T) {
	// New() must uppercase the first rune to satisfy Weaviate conventions.
	// We verify only that construction succeeds; the internal class name
	// is used by methods that would require a live server to observe.
	s, err := weaviate.New(fakeClient(), "myCollection")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Store")
	}
}

// -- Upsert validation tests -------------------------------------------------

func TestUpsert_EmptyPointsReturnsError(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = s.Upsert(context.Background(), nil)
	if !errors.Is(err, vectorstore.ErrEmptyPoints) {
		t.Fatalf("expected ErrEmptyPoints, got %v", err)
	}
}

func TestUpsert_EmptyPointIDReturnsError(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	points := []vectorstore.Point{
		{ID: "", Vector: []float32{1, 2, 3, 4}},
	}
	err = s.Upsert(context.Background(), points)
	if !errors.Is(err, weaviate.ErrPointIDRequired) {
		t.Fatalf("expected ErrPointIDRequired, got %v", err)
	}
}

func TestUpsert_EmptyVectorReturnsError(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	points := []vectorstore.Point{
		{ID: "p1", Vector: nil},
	}
	err = s.Upsert(context.Background(), points)

	var emptyVecErr *weaviate.EmptyVectorError
	if !errors.As(err, &emptyVecErr) {
		t.Fatalf("expected EmptyVectorError, got %v", err)
	}
	if emptyVecErr.PointID != "p1" {
		t.Fatalf("expected PointID %q, got %q", "p1", emptyVecErr.PointID)
	}
}

func TestUpsert_VectorSizeMismatchReturnsError(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col", weaviate.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	points := []vectorstore.Point{
		{ID: "p1", Vector: []float32{1, 2}}, // 2 instead of 4
	}
	err = s.Upsert(context.Background(), points)

	var mismatch *weaviate.VectorSizeMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected VectorSizeMismatchError, got %v", err)
	}
	if mismatch.Expected != 4 || mismatch.Got != 2 {
		t.Fatalf("mismatch fields: expected=%d got=%d", mismatch.Expected, mismatch.Got)
	}
}

// -- Query validation tests --------------------------------------------------

func TestQuery_EmptyQueryReturnsError(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Query(context.Background(), nil, 5)
	if !errors.Is(err, vectorstore.ErrEmptyQuery) {
		t.Fatalf("expected ErrEmptyQuery, got %v", err)
	}
}

func TestQuery_VectorSizeMismatchReturnsError(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col", weaviate.WithVectorSize(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = s.Query(context.Background(), []float32{1, 2}, 5) // 2 instead of 4
	var mismatch *weaviate.VectorSizeMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected VectorSizeMismatchError, got %v", err)
	}
}

// -- Delete validation tests -------------------------------------------------

func TestDelete_EmptyIDsReturnsError(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	err = s.Delete(context.Background(), nil)
	if !errors.Is(err, vectorstore.ErrEmptyIDs) {
		t.Fatalf("expected ErrEmptyIDs, got %v", err)
	}
}

// -- option tests ------------------------------------------------------------

func TestWithBatchSize_Option(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col", weaviate.WithBatchSize(50))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Store")
	}
}

func TestWithDistance_Option(t *testing.T) {
	s, err := weaviate.New(fakeClient(), "Col", weaviate.WithDistance(weaviate.DistanceDot))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil Store")
	}
}
