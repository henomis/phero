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

package qdrant

import (
	"errors"
	"fmt"
)

var (
	// ErrNilClient is returned when New receives a nil Qdrant client.
	ErrNilClient = errors.New("nil qdrant client")
	// ErrEmptyCollection is returned when New receives an empty collection name.
	ErrEmptyCollection = errors.New("empty collection")

	// ErrInvalidVectorSize is returned when New receives an invalid vector size.
	ErrInvalidVectorSize = errors.New("invalid vector size")

	// ErrPointIDRequired is returned when a point has an empty ID.
	ErrPointIDRequired = errors.New("point id is required")
)

// VectorSizeMismatchError is returned when a point/query vector length does not
// match the configured collection vector size.
type VectorSizeMismatchError struct {
	Expected uint64
	Got      int
}

func (e *VectorSizeMismatchError) Error() string {
	return fmt.Sprintf("vector size mismatch: expected %d, got %d", e.Expected, e.Got)
}

// EmptyVectorError is returned when a point has an empty vector.
type EmptyVectorError struct {
	PointID string
}

func (e *EmptyVectorError) Error() string {
	return fmt.Sprintf("empty vector for point id %q", e.PointID)
}

// InvalidPayloadError is returned when a point payload cannot be encoded into
// a Qdrant payload.
type InvalidPayloadError struct {
	PointID string
	Err     error
}

func (e *InvalidPayloadError) Error() string {
	return fmt.Sprintf("invalid payload for point id %q: %v", e.PointID, e.Err)
}

func (e *InvalidPayloadError) Unwrap() error {
	return e.Err
}
