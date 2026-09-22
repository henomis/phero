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
	"time"
)

// readyFlushTimeout bounds the flush that turns "registered" into "subscribed
// on the server". It is a round trip to NATS, not work: a connection that
// cannot answer within this is not one an agent can serve on.
const readyFlushTimeout = 10 * time.Second

// Ready returns a channel closed once this server is registered and its
// endpoints are live on the NATS server — the moment a prompt sent to it is
// answered rather than dropped.
//
// [Server.Start] blocks for the serving lifetime, so a caller runs it in a
// goroutine and has, until now, had no way to learn when that goroutine got the
// agent up. The usual stand-in is to poll [Client.Discover] until the name
// appears, which costs a request round trip per attempt and reports "visible in
// discovery", not "subscribed". An agent started per unit of work — one
// instance per session, per job, per tenant — pays that on every start, on the
// path a caller is waiting on.
//
// The channel is never closed if the server stops before it serves; use
// [Server.WaitReady] to be woken by that too.
func (s *Server) Ready() <-chan struct{} { return s.servingChan() }

// WaitReady blocks until this server is serving (see [Server.Ready]), ctx ends,
// or the server stops before it got there — a registration error, or a Start on
// a server that has already drained. It returns nil, ctx.Err(), or the reason
// the server never came up.
//
// A drain after the server came up does not make WaitReady return an error: the
// question it answers is "did this server reach serving", asked once, at start.
func (s *Server) WaitReady(ctx context.Context) error {
	serving, stopped := s.servingChan(), s.stoppedChan()

	select {
	case <-serving:
		return nil
	default:
	}

	select {
	case <-serving:
		return nil
	case <-stopped:
		// Both can be closed — a server that came up and then drained — and the
		// select picks between them at random. Serving wins: it happened first,
		// and the question is whether this server ever came up.
		select {
		case <-serving:
			return nil
		default:
			return s.startError()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

// markServing records that registration completed. It is idempotent: a Server
// serves once, so the channel closes once.
func (s *Server) markServing() {
	ch := s.servingChan()
	s.servingOnce.Do(func() { close(ch) })
}

// startFailed records why Start gave up before serving and wakes anything
// waiting on readiness, then returns err unchanged so Start can return it.
func (s *Server) startFailed(err error) error {
	s.mu.Lock()
	if s.startErr == nil {
		s.startErr = err
	}

	ch := s.stoppedLocked()
	s.mu.Unlock()

	s.stoppedOnce.Do(func() { close(ch) })

	return err
}

// The readiness channels are created on first use rather than in New, so a
// Server built as a struct literal — as the package's own drain tests do —
// behaves like any other.
func (s *Server) servingChan() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.serving == nil {
		s.serving = make(chan struct{})
	}

	return s.serving
}

func (s *Server) stoppedChan() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.stoppedLocked()
}

// stoppedLocked returns the stopped channel; callers hold s.mu.
func (s *Server) stoppedLocked() chan struct{} {
	if s.stopped == nil {
		s.stopped = make(chan struct{})
	}

	return s.stopped
}

// startError reports why the server stopped before serving.
func (s *Server) startError() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.startErr != nil {
		return s.startErr
	}

	return ErrServerStopped
}
