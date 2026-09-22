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
	"errors"
	"testing"
	"time"
)

func TestWaitReadyReturnsOnceServing(t *testing.T) {
	s, _ := newTestServer(t, time.Second)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	go func() {
		time.Sleep(10 * time.Millisecond)
		s.markServing()
	}()

	if err := s.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v, want nil once the server serves", err)
	}

	select {
	case <-s.Ready():
	default:
		t.Error("Ready() must be closed once the server serves")
	}

	// Asking again is free and still true.
	if err := s.WaitReady(ctx); err != nil {
		t.Errorf("second WaitReady: %v, want nil", err)
	}
}

// A server that never comes up must wake its waiters. Otherwise a caller that
// starts an agent per unit of work learns about a registration failure only
// when its own timeout expires.
func TestWaitReadyReturnsStartFailure(t *testing.T) {
	s, _ := newTestServer(t, time.Second)

	wantErr := errors.New("register failed")

	go func() {
		time.Sleep(10 * time.Millisecond)

		_ = s.startFailed(wantErr)
	}()

	err := s.WaitReady(t.Context())

	if !errors.Is(err, wantErr) {
		t.Fatalf("WaitReady = %v, want %v", err, wantErr)
	}
}

// A Server serves once, so one that drains without having served never will.
func TestWaitReadyReturnsAfterDrain(t *testing.T) {
	s, _ := newTestServer(t, 0)

	if err := s.Drain(t.Context()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	err := s.WaitReady(t.Context())
	if !errors.Is(err, ErrServerStopped) {
		t.Fatalf("WaitReady = %v, want ErrServerStopped", err)
	}
}

// Draining a server that did come up is an ordinary shutdown, not a failure to
// start: WaitReady still reports that it served.
func TestWaitReadyAfterServingThenDraining(t *testing.T) {
	s, _ := newTestServer(t, 0)
	s.markServing()

	if err := s.Drain(t.Context()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if err := s.WaitReady(t.Context()); err != nil {
		t.Errorf("WaitReady = %v, want nil for a server that served and then drained", err)
	}
}

func TestWaitReadyRespectsContext(t *testing.T) {
	s, _ := newTestServer(t, time.Second)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if err := s.WaitReady(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitReady = %v, want DeadlineExceeded", err)
	}
}
