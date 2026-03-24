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

package simple

import (
	"sync"
)

type ringBuffer[T any] struct {
	buffer []T
	size   int
	mu     sync.RWMutex
	write  int
	count  int
}

// newRingBuffer creates a new ring buffer with a fixed size.
func newRingBuffer[T any](size int) *ringBuffer[T] {
	return &ringBuffer[T]{
		buffer: make([]T, size),
		size:   size,
	}
}

// Add inserts a new element into the buffer, overwriting the oldest if full.
func (rb *ringBuffer[T]) Add(value T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buffer[rb.write] = value
	rb.write = (rb.write + 1) % rb.size

	if rb.count < rb.size {
		rb.count++
	}
}

func (rb *ringBuffer[T]) Replace(values []T) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buffer = make([]T, rb.size)
	rb.write = 0
	rb.count = 0

	for _, value := range values {
		rb.buffer[rb.write] = value
		rb.write = (rb.write + 1) % rb.size

		if rb.count < rb.size {
			rb.count++
		}
	}
}

// Get returns the contents of the buffer in FIFO order.
func (rb *ringBuffer[T]) Get() []T {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	result := make([]T, 0, rb.count)

	for i := 0; i < rb.count; i++ {
		index := (rb.write + rb.size - rb.count + i) % rb.size
		result = append(result, rb.buffer[index])
	}

	return result
}

// Len returns the current number of elements in the buffer.
func (rb *ringBuffer[T]) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Clear resets the buffer to an empty state.
func (rb *ringBuffer[T]) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.buffer = make([]T, rb.size)
	rb.write = 0
	rb.count = 0
}
