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

package natsmemory

import (
	"errors"
	"fmt"
)

var (
	// ErrNilKeyValue is returned when the NATS key-value store is nil.
	ErrNilKeyValue = errors.New("nil key-value store")
	// ErrEmptySessionID is returned when the session ID is empty.
	ErrEmptySessionID = errors.New("empty session id")
	// ErrConcurrentUpdate is returned by Save when other writers kept updating
	// the session between its read and its write, on every attempt.
	ErrConcurrentUpdate = errors.New("session updated concurrently")
	// ErrSessionTooLarge is returned by Save when the session no longer fits in
	// one NATS message; see [SessionTooLargeError].
	ErrSessionTooLarge = errors.New("session too large to store")
)

// SessionTooLargeError is returned by Save when the encoded session is larger
// than NATS will store as one value: the connection's max_payload, or the
// bucket's MaxValueSize when that is smaller. The session is stored as a whole,
// so every later Save to it fails the same way until it shrinks — see the
// package documentation for what to do. It matches [ErrSessionTooLarge] with
// errors.Is, and wraps the NATS error.
type SessionTooLargeError struct {
	// Session is the session ID.
	Session string
	// Size is the encoded size, in bytes, that did not fit.
	Size int
	// Err is the error NATS returned.
	Err error
}

func (e *SessionTooLargeError) Error() string {
	return fmt.Sprintf("%s: session %q is %d bytes: %v", ErrSessionTooLarge, e.Session, e.Size, e.Err)
}

// Is reports ErrSessionTooLarge, so errors.Is matches it.
func (e *SessionTooLargeError) Is(target error) bool { return target == ErrSessionTooLarge }

// Unwrap returns the NATS error.
func (e *SessionTooLargeError) Unwrap() error { return e.Err }
