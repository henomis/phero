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
	"errors"
	"fmt"
)

// EmbedderVectorCountMismatchError is returned when an embedder returns a
// different number of vectors than requested.
type EmbedderVectorCountMismatchError struct {
	Got         int
	Want        int
	SingleQuery bool
}

func (e *EmbedderVectorCountMismatchError) Error() string {
	if e.SingleQuery {
		return fmt.Sprintf("embedder returned %d vectors for single query", e.Got)
	}
	return fmt.Sprintf("embedder returned %d vectors for %d texts", e.Got, e.Want)
}

// ErrNilStore is returned when New receives a nil Store.
var ErrNilStore = errors.New("nil store")

// ErrNilEmbedder is returned when New receives a nil Embedder.
var ErrNilEmbedder = errors.New("nil embedder")

// ErrEmptyTexts is returned when an ingest operation receives no texts.
var ErrEmptyTexts = errors.New("empty texts")

// ErrEmptyQueryText is returned when a query-by-text operation receives an empty query string.
var ErrEmptyQueryText = errors.New("empty query text")

// IngestError is returned when an ingest operation fails at the embed or upsert step.
// Op is "embed" or "upsert". BatchStart and BatchEnd identify the failing text slice.
type IngestError struct {
	Op         string
	BatchStart int
	BatchEnd   int
	Cause      error
}

func (e *IngestError) Error() string {
	return fmt.Sprintf("ingest: %s batch [%d:%d]: %v", e.Op, e.BatchStart, e.BatchEnd, e.Cause)
}

func (e *IngestError) Unwrap() error { return e.Cause }

// QueryError is returned when a query operation fails at the embed or store-query step.
// Op is "embed" or "store query".
type QueryError struct {
	Op    string
	Cause error
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("query: %s: %v", e.Op, e.Cause)
}

func (e *QueryError) Unwrap() error { return e.Cause }
