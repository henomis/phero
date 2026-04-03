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
	"context"
	"sync"
	"time"

	"github.com/henomis/phero/llm"
)

// pendingRequest holds one queued Execute call along with the channel used to
// deliver its response back to the caller.
type pendingRequest struct {
	ctx      context.Context
	messages []llm.Message
	tools    []*llm.Tool
	replyCh  chan replyEnvelope
}

// replyEnvelope carries the outcome of a dispatched Execute call.
type replyEnvelope struct {
	result *llm.Result
	err    error
}

// rateLimitedLLM is the concrete LLM produced by the RateLimit middleware. It
// serialises Execute calls to inner, dispatching at most one per ticker interval.
type rateLimitedLLM struct {
	inner  llm.LLM
	queue  chan *pendingRequest
	stopCh chan struct{}
}

// Execute queues the request and blocks until it is dispatched, its context is
// cancelled, or the middleware is stopped.
func (r *rateLimitedLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	req := &pendingRequest{
		ctx:      ctx,
		messages: messages,
		tools:    tools,
		replyCh:  make(chan replyEnvelope, 1),
	}

	select {
	case r.queue <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.stopCh:
		return nil, ErrStopped
	}

	select {
	case reply := <-req.replyCh:
		return reply.result, reply.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// run is the dispatch loop. It fires once per interval, picks the next queued
// request, and forwards it to inner. It exits when the stop channel is closed.
func (r *rateLimitedLLM) run(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			r.drainQueue()
			return
		case <-ticker.C:
			select {
			case req := <-r.queue:
				r.dispatch(req)
			case <-r.stopCh:
				r.drainQueue()
				return
			default:
				// nothing queued; wait for the next tick
			}
		}
	}
}

// dispatch executes req against inner and delivers the result. If the request
// context is already cancelled, the call is short-circuited.
func (r *rateLimitedLLM) dispatch(req *pendingRequest) {
	if err := req.ctx.Err(); err != nil {
		req.replyCh <- replyEnvelope{err: err}
		return
	}

	result, err := r.inner.Execute(req.ctx, req.messages, req.tools)
	req.replyCh <- replyEnvelope{result: result, err: err}
}

// drainQueue unblocks all pending callers with ErrStopped.
func (r *rateLimitedLLM) drainQueue() {
	for {
		select {
		case req := <-r.queue:
			req.replyCh <- replyEnvelope{err: ErrStopped}
		default:
			return
		}
	}
}

// NewRateLimit returns an llm.LLMMiddleware that limits Execute calls to at
// most requestsPerSecond per second. bufferSize controls how many calls may be
// queued before callers block.
//
// The returned stop function must be called to shut down the background
// goroutine and unblock any pending callers. It is safe to call stop more than
// once.
//
// mw, stop, err := middleware.NewRateLimit(2.0, 10)
// if err != nil { ... }
// defer stop()
//
// client := llm.Use(openaiClient, mw)
func NewRateLimit(requestsPerSecond float64, bufferSize int) (llm.LLMMiddleware, func(), error) {
	if requestsPerSecond <= 0 {
		return nil, nil, ErrInvalidRate
	}
	if bufferSize < 0 {
		return nil, nil, ErrInvalidBufferSize
	}

	interval := time.Duration(float64(time.Second) / requestsPerSecond)
	stopCh := make(chan struct{})
	var stopOnce sync.Once

	stop := func() {
		stopOnce.Do(func() { close(stopCh) })
	}

	mw := func(next llm.LLM) llm.LLM {
		r := &rateLimitedLLM{
			inner:  next,
			queue:  make(chan *pendingRequest, bufferSize),
			stopCh: stopCh,
		}
		go r.run(interval)
		return r
	}

	return mw, stop, nil
}
