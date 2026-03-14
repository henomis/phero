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

package vectorstore

import (
	"context"
)

// Vector is a vector embedding.
type Vector = []float32

// Point is a vector point to be stored.
type Point struct {
	ID      string
	Vector  Vector
	Payload map[string]any
}

// ScoredPoint is a query result.
type ScoredPoint struct {
	ID      string
	Score   float32
	Payload map[string]any
}

// Store persists vectors and supports similarity search.
//
// Implementations are typically bound to a single collection.
type Store interface {
	// EnsureCollection ensures the backing collection exists.
	EnsureCollection(ctx context.Context) error

	// Upsert inserts or updates points.
	Upsert(ctx context.Context, points []Point) error

	// Query returns the top-k nearest points to query.
	Query(ctx context.Context, query Vector, limit uint64) ([]ScoredPoint, error)

	// Delete deletes points by ID.
	Delete(ctx context.Context, ids []string) error

	// Clear deletes all points in the collection.
	Clear(ctx context.Context) error
}
