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

package nats

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// The drain budget is a total, not a first instalment: [WithDrainTimeout]
// bounds the whole shutdown, so an embedder sizing it against its own budget
// never has to know there are two phases. It is therefore split rather than
// extended — graceGranularity reserves the last 1/n of the budget for handlers
// to notice their cancellation and unwind, and the rest is the graceful wait.
const graceGranularity = 5

// drainCancelGrace is the unwind wait when the budget is zero. Zero means "do
// not wait for the work"; it cannot also mean "do not wait for the goroutine",
// so that one case gets a fixed grace rather than a share of nothing.
const drainCancelGrace = 5 * time.Second

// drainPhases splits a drain budget into the graceful wait and the wait that
// follows cancellation.
func drainPhases(budget time.Duration) (graceful, unwind time.Duration) {
	if budget <= 0 {
		return 0, drainCancelGrace
	}

	unwind = budget / graceGranularity

	return budget - unwind, unwind
}

// gate is a [Server]'s "accepting new prompts?" state, and the count of the
// handlers already running under it.
//
// It exists so that stopping a server is two steps rather than one. Cancelling
// the context a handler runs on abandons an LLM call that has already been paid
// for; unsubscribing its endpoint does not. A gate lets a drain do the second
// first — stop admitting work, let what is in flight finish — and reach for
// cancellation only when the budget for that is spent.
//
// The gate opens once and closes once. A closed gate never reopens: a Server
// that has drained is finished.
type gate struct {
	mu       sync.Mutex
	open     bool
	closed   bool
	inflight int
	idle     chan struct{}
}

func newGate() *gate {
	return &gate{idle: make(chan struct{})}
}

// unlock starts admitting prompts. It is a no-op once the gate has closed, so a
// drain that races a late [Server.Start] still wins.
func (g *gate) unlock() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.closed {
		g.open = true
	}
}

// enter registers one in-flight handler. It reports false when the gate is not
// admitting work, in which case the caller must reject the request rather than
// serve it — and must not call leave.
func (g *gate) enter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.open || g.closed {
		return false
	}

	g.inflight++

	return true
}

// leave releases one in-flight handler, signalling idle when it was the last
// one a closing gate was waiting for.
func (g *gate) leave() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.inflight--

	if g.closed && g.inflight == 0 {
		g.signalIdle()
	}
}

// close stops admitting prompts and returns a channel that is closed once the
// last in-flight handler has left — already closed when there are none.
//
// Closing the gate before unsubscribing is what makes the drain race-free: the
// micro service's Stop only *drains* its subscriptions, so a request already
// buffered can still reach the handler afterwards. Such a request finds the gate
// shut and is refused, rather than being admitted into a count nobody is waiting
// on any more.
func (g *gate) close() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.closed = true

	if g.inflight == 0 {
		g.signalIdle()
	}

	return g.idle
}

// inFlight reports how many handlers are currently running.
func (g *gate) inFlight() int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.inflight
}

// signalIdle closes the idle channel at most once. Callers hold g.mu.
func (g *gate) signalIdle() {
	select {
	case <-g.idle:
	default:
		close(g.idle)
	}
}

// Drain stops the server accepting new prompts and waits for the handlers
// already running to finish.
//
// The order matters, and it is the reason this method exists. First the prompt
// endpoint is unsubscribed, which removes this instance from the "agents" queue
// group so the NATS server routes further prompts to another replica; a prompt
// that was already buffered here is answered 503 rather than served. Heartbeats
// stop, so liveness stops advertising an instance that no longer takes work.
// Only then does Drain wait — on a context the shutdown has *not* cancelled — so
// an agent in the middle of an LLM call finishes it and returns its answer.
//
// The drain timeout (see [WithDrainTimeout], default 30s) bounds the whole of
// this: handlers still running when the graceful share of it is spent are
// cancelled, and the remainder is theirs to unwind in. Drain returns
// [ErrDrainIncomplete] if they do not. A zero or negative drain timeout skips
// the graceful wait entirely and cancels immediately.
//
// Drain also ends [Server.Start]: a Start blocked on its context returns once
// the drain completes. It is safe to call concurrently and more than once —
// every caller observes the same drain, and ctx bounds only this caller's wait,
// not the drain itself.
func (s *Server) Drain(ctx context.Context) error {
	s.drainOnce.Do(func() { go s.runDrain() })

	select {
	case <-s.drained:
		return s.drainErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runDrain performs the drain exactly once. drainErr is written before drained
// is closed, so every Drain caller observes it.
func (s *Server) runDrain() {
	defer close(s.drained)

	// Stop admitting before anything else, so the set of handlers to wait for
	// cannot grow underneath the wait.
	idle := s.gate.close()

	s.mu.Lock()
	s.draining = true
	svc, stopServing, cancelPrompts := s.svc, s.stopServing, s.cancelPrompts
	s.mu.Unlock()

	if svc != nil {
		_ = svc.Stop()
	}

	// End the serving lifetime: the heartbeat publisher exits and a blocked
	// Start unblocks. In-flight handlers are unaffected — they run on a lifetime
	// of their own, which is the point.
	if stopServing != nil {
		stopServing()
	}

	graceful, unwind := drainPhases(s.cfg.drainTimeout)

	if waitIdle(idle, graceful) {
		s.hbWG.Wait()

		return
	}

	if cancelPrompts != nil {
		cancelPrompts()
	}

	if !waitIdle(idle, unwind) {
		s.drainErr = fmt.Errorf("%w: %d handler(s) still running", ErrDrainIncomplete, s.gate.inFlight())
	}

	s.hbWG.Wait()
}

// waitIdle waits up to budget for idle to close, reporting whether it did. A
// non-positive budget is not a wait at all: it reports the current state, so
// WithDrainTimeout(0) means "cancel in-flight handlers immediately".
func waitIdle(idle <-chan struct{}, budget time.Duration) bool {
	if budget <= 0 {
		select {
		case <-idle:
			return true
		default:
			return false
		}
	}

	timer := time.NewTimer(budget)
	defer timer.Stop()

	select {
	case <-idle:
		return true
	case <-timer.C:
		return false
	}
}

// hasDrained reports whether a drain has already completed.
func (s *Server) hasDrained() bool {
	select {
	case <-s.drained:
		return true
	default:
		return false
	}
}
