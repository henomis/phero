package openai

import "fmt"

// ResponseIndexOutOfRangeError is returned when an embeddings response contains
// an item index that does not fit in the expected output slice.
type ResponseIndexOutOfRangeError struct {
	Index int
	Len   int
}

func (e *ResponseIndexOutOfRangeError) Error() string {
	return fmt.Sprintf("embedding response index out of range: %d (len=%d)", e.Index, e.Len)
}

// MissingEmbeddingError is returned when an embeddings response does not contain
// an embedding for a requested input index.
type MissingEmbeddingError struct {
	Index int
}

func (e *MissingEmbeddingError) Error() string {
	return fmt.Sprintf("missing embedding for input index %d", e.Index)
}
