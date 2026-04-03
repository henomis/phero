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

package middleware

import (
	"errors"
	"fmt"
)

// ErrInvalidRate is returned when requestsPerSecond is not greater than zero.
var ErrInvalidRate = errors.New("middleware: rate must be greater than zero")

// ErrInvalidBufferSize is returned when bufferSize is negative.
var ErrInvalidBufferSize = errors.New("middleware: buffer size must be non-negative")

// ErrStopped is returned to pending callers when Stop is called on the middleware.
var ErrStopped = errors.New("middleware: LLM middleware has been stopped")

// ErrInvalidMaxAttempts is returned when maxAttempts is less than 1.
var ErrInvalidMaxAttempts = errors.New("middleware: maxAttempts must be at least 1")

// MaxAttemptsExceededError is returned by the Retry middleware when all
// attempts have been exhausted. Unwrapping it yields the last underlying error.
type MaxAttemptsExceededError struct {
	// Attempts is the total number of attempts that were made.
	Attempts int
	// Err is the last error returned by the inner LLM.
	Err error
}

// Error implements the error interface.
func (e *MaxAttemptsExceededError) Error() string {
	return fmt.Sprintf("middleware: all %d attempts failed: %v", e.Attempts, e.Err)
}

// Unwrap returns the last underlying error so callers can use errors.Is/As.
func (e *MaxAttemptsExceededError) Unwrap() error {
	return e.Err
}
