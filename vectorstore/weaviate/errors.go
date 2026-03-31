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

package weaviate

import (
	"errors"
	"fmt"
)

var (
	// ErrNilClient is returned when New receives a nil Weaviate client.
	ErrNilClient = errors.New("nil weaviate client")
	// ErrEmptyCollection is returned when New receives an empty class name.
	ErrEmptyCollection = errors.New("empty collection")
	// ErrPointIDRequired is returned when a point has an empty ID.
	ErrPointIDRequired = errors.New("point id is required")
)

// VectorSizeMismatchError is returned when a point or query vector length does
// not match the configured vector size.
type VectorSizeMismatchError struct {
	// Expected is the configured vector size.
	Expected uint64
	// Got is the actual vector length provided.
	Got int
}

// Error implements the error interface.
func (e *VectorSizeMismatchError) Error() string {
	return fmt.Sprintf("vector size mismatch: expected %d, got %d", e.Expected, e.Got)
}

// EmptyVectorError is returned when a point has an empty vector.
type EmptyVectorError struct {
	// PointID identifies which point had the empty vector.
	PointID string
}

// Error implements the error interface.
func (e *EmptyVectorError) Error() string {
	return fmt.Sprintf("empty vector for point %q", e.PointID)
}

// Unwrap returns nil; EmptyVectorError is a leaf error.
func (e *EmptyVectorError) Unwrap() error { return nil }
