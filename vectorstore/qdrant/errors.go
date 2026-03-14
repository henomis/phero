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
