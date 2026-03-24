package psql

import (
	"errors"
	"fmt"
)

var (
	// ErrNilDB is returned when New receives a nil *sql.DB.
	ErrNilDB = errors.New("nil db")
	// ErrEmptyCollection is returned when New receives an empty collection name.
	ErrEmptyCollection = errors.New("empty collection")
	// ErrEmptyTableName is returned when WithTable (or internal defaults) result
	// in an empty table name.
	ErrEmptyTableName = errors.New("empty table name")
	// ErrInvalidVectorSize is returned when the vector size is missing or invalid.
	ErrInvalidVectorSize = errors.New("invalid vector size")
	// ErrInvalidTableName is returned when the configured table name is not a safe SQL identifier.
	ErrInvalidTableName = errors.New("invalid table name")
	// ErrPointIDRequired is returned when a point has an empty ID.
	ErrPointIDRequired = errors.New("point id is required")
)

// VectorSizeMismatchError is returned when a point/query vector length does not
// match the configured vector size.
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

// InvalidVectorValueError is returned when a vector contains NaN/Inf.
type InvalidVectorValueError struct {
	Index int
	Value float32
}

func (e *InvalidVectorValueError) Error() string {
	return fmt.Sprintf("invalid vector value at index %d: %v", e.Index, e.Value)
}

// PayloadDecodeError is returned when a stored JSON payload cannot be
// unmarshalled for a given point ID.
type PayloadDecodeError struct {
	PointID string
	Cause   error
}

func (e *PayloadDecodeError) Error() string {
	return fmt.Sprintf("decode payload for point %q: %v", e.PointID, e.Cause)
}

func (e *PayloadDecodeError) Unwrap() error { return e.Cause }
