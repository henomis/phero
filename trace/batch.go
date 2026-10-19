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

package trace

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// defaultBatchQueueSize is the default capacity of the Batch queue.
const defaultBatchQueueSize = 2048

// BatchOption configures Batch.
type BatchOption func(*batchProcessor)

// WithQueueSize sets the number of ended spans Batch buffers. When the queue
// is full new spans are dropped and counted (see BatchProcessor.Dropped).
func WithQueueSize(n int) BatchOption {
	return func(b *batchProcessor) {
		if n > 0 {
			b.size = n
		}
	}
}

// BatchProcessor is the processor returned by Batch.
type BatchProcessor interface {
	Processor
	// Dropped returns the number of spans dropped because the queue was full.
	Dropped() uint64
}

// Batch returns a processor that hands ended spans to next on a background
// goroutine, so that exporters doing I/O don't slow down the agent. OnStart
// calls are forwarded synchronously. Shutdown drains the queue and then shuts
// next down.
func Batch(next Processor, opts ...BatchOption) BatchProcessor {
	b := &batchProcessor{next: next, size: defaultBatchQueueSize, done: make(chan struct{})}
	for _, opt := range opts {
		opt(b)
	}

	b.queue = make(chan *SpanData, b.size)

	go b.loop()

	return b
}

type batchProcessor struct {
	next    Processor
	size    int
	queue   chan *SpanData
	done    chan struct{}
	dropped atomic.Uint64

	mu     sync.RWMutex
	closed bool
}

func (b *batchProcessor) loop() {
	defer close(b.done)

	for d := range b.queue {
		b.next.OnEnd(d)
	}
}

func (b *batchProcessor) OnStart(ctx context.Context, s *SpanData) {
	b.next.OnStart(ctx, s)
}

func (b *batchProcessor) OnEnd(s *SpanData) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		b.dropped.Add(1)
		return
	}

	select {
	case b.queue <- s:
	default:
		b.dropped.Add(1)
	}
}

func (b *batchProcessor) Dropped() uint64 { return b.dropped.Load() }

func (b *batchProcessor) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	if !b.closed {
		b.closed = true
		close(b.queue)
	}
	b.mu.Unlock()

	select {
	case <-b.done:
	case <-ctx.Done():
		return errors.Join(ctx.Err(), b.next.Shutdown(ctx))
	}

	return b.next.Shutdown(ctx)
}
