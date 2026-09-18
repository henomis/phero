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
	"sync"
	"testing"
	"time"
)

// newTestServer builds a Server with no NATS behind it: Drain is nil-safe on
// s.svc, so the whole shutdown discipline can be exercised without a broker.
// The returned context is the one in-flight handlers would run on, so a test can
// assert whether the drain cancelled them.
func newTestServer(t *testing.T, drainTimeout time.Duration) (*Server, context.Context) {
	t.Helper()

	cfg := defaultServerConfig()
	cfg.drainTimeout = drainTimeout

	promptCtx, cancelPrompts := context.WithCancel(context.Background())
	_, stopServing := context.WithCancel(context.Background())

	t.Cleanup(cancelPrompts)
	t.Cleanup(stopServing)

	s := &Server{
		cfg:           cfg,
		owner:         "acme",
		name:          "worker",
		gate:          newGate(),
		drained:       make(chan struct{}),
		stopServing:   stopServing,
		cancelPrompts: cancelPrompts,
	}
	s.gate.unlock()

	return s, promptCtx
}

func TestGateAdmitsThenRefuses(t *testing.T) {
	g := newGate()

	if g.enter() {
		t.Fatal("a gate that was never unlocked must not admit")
	}

	g.unlock()

	if !g.enter() {
		t.Fatal("an open gate must admit")
	}

	g.close()

	if g.enter() {
		t.Fatal("a closed gate must refuse, so a buffered request is 503'd rather than served")
	}

	// The handler admitted before the close is still counted, and must be.
	if got := g.inFlight(); got != 1 {
		t.Errorf("inFlight = %d, want 1", got)
	}
}

func TestGateIdleWaitsForInFlight(t *testing.T) {
	g := newGate()
	g.unlock()

	for i := range 2 {
		if !g.enter() {
			t.Fatalf("enter %d should succeed while open", i)
		}
	}

	idle := g.close()

	select {
	case <-idle:
		t.Fatal("idle closed while two handlers are still in flight")
	default:
	}

	g.leave()

	select {
	case <-idle:
		t.Fatal("idle closed with one handler still in flight")
	default:
	}

	g.leave()

	select {
	case <-idle:
	case <-time.After(time.Second):
		t.Fatal("idle did not close after the last handler left")
	}
}

func TestGateCloseWithNothingInFlightIsIdleAtOnce(t *testing.T) {
	g := newGate()
	g.unlock()

	select {
	case <-g.close():
	default:
		t.Fatal("closing an empty gate must be idle immediately")
	}
}

func TestGateDoesNotReopenAfterClose(t *testing.T) {
	g := newGate()
	g.close()
	g.unlock()

	if g.enter() {
		t.Fatal("unlock must not reopen a gate that has drained")
	}
}

// TestDrainLetsInFlightWorkFinish is the point of the whole change: a shutdown
// waits for a handler that is already running, and does not cancel it while it
// waits. Before this existed, cancelling the server cancelled the handler's
// context in the same instant, so an LLM call already paid for was abandoned.
func TestDrainLetsInFlightWorkFinish(t *testing.T) {
	s, promptCtx := newTestServer(t, 5*time.Second)

	if !s.gate.enter() {
		t.Fatal("gate should admit before the drain")
	}

	finished := make(chan struct{})

	go func() {
		defer close(finished)
		defer s.gate.leave()

		// Stand in for an LLM call in progress. If the drain cancelled us, the
		// context would be done well before this elapses.
		time.Sleep(150 * time.Millisecond)
	}()

	start := time.Now()

	if err := s.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Errorf("Drain returned after %s; it did not wait for the handler", elapsed)
	}

	select {
	case <-finished:
	default:
		t.Fatal("Drain returned while the handler was still running")
	}

	if err := promptCtx.Err(); err != nil {
		t.Errorf("handler context was cancelled during a drain that had budget left: %v", err)
	}
}

// TestDrainStopsAcceptingBeforeWaiting pins the ordering: acceptance ends at the
// start of the drain, not at the end. A prompt arriving during the graceful
// window belongs to another replica.
func TestDrainStopsAcceptingBeforeWaiting(t *testing.T) {
	s, _ := newTestServer(t, 5*time.Second)

	if !s.gate.enter() {
		t.Fatal("gate should admit before the drain")
	}

	admitted := make(chan bool, 1)

	go func() {
		// Give the drain time to reach its wait, then try to get in.
		time.Sleep(50 * time.Millisecond)

		admitted <- s.gate.enter()

		s.gate.leave()
	}()

	if err := s.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if <-admitted {
		t.Error("a prompt was admitted after the drain began")
	}
}

// TestDrainCancelsWhenBudgetSpent covers the other half: the wait is bounded, so
// a handler that will not finish does not hold the shutdown open forever.
func TestDrainCancelsWhenBudgetSpent(t *testing.T) {
	s, promptCtx := newTestServer(t, 50*time.Millisecond)

	if !s.gate.enter() {
		t.Fatal("gate should admit before the drain")
	}

	go func() {
		defer s.gate.leave()

		<-promptCtx.Done() // only cancellation gets this handler to return
	}()

	if err := s.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if err := promptCtx.Err(); err == nil {
		t.Error("handler context was not cancelled after the drain budget was spent")
	}
}

// TestDrainReportsIncomplete pins the honest outcome for a handler that ignores
// cancellation: the drain still returns, and says it did not finish.
func TestDrainReportsIncomplete(t *testing.T) {
	s, _ := newTestServer(t, 20*time.Millisecond)

	if !s.gate.enter() {
		t.Fatal("gate should admit before the drain")
	}

	// Deliberately never leaves: a handler that ignores its context.
	defer s.gate.leave()

	err := s.Drain(context.Background())
	if !errors.Is(err, ErrDrainIncomplete) {
		t.Fatalf("Drain error = %v, want ErrDrainIncomplete", err)
	}
}

func TestDrainZeroTimeoutCancelsImmediately(t *testing.T) {
	s, promptCtx := newTestServer(t, 0)

	if !s.gate.enter() {
		t.Fatal("gate should admit before the drain")
	}

	go func() {
		defer s.gate.leave()

		<-promptCtx.Done()
	}()

	start := time.Now()

	if err := s.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("a zero drain timeout waited %s; it should cancel at once", elapsed)
	}

	if err := promptCtx.Err(); err == nil {
		t.Error("a zero drain timeout must cancel in-flight handlers")
	}
}

// TestDrainIsIdempotent: every caller observes the same drain and the same
// outcome, and the drain itself runs once.
func TestDrainIsIdempotent(t *testing.T) {
	s, _ := newTestServer(t, 20*time.Millisecond)

	if !s.gate.enter() {
		t.Fatal("gate should admit before the drain")
	}

	defer s.gate.leave()

	var wg sync.WaitGroup

	errs := make([]error, 4)

	for i := range errs {
		wg.Go(func() { errs[i] = s.Drain(context.Background()) })
	}

	wg.Wait()

	for i, err := range errs {
		if !errors.Is(err, ErrDrainIncomplete) {
			t.Errorf("caller %d: Drain error = %v, want ErrDrainIncomplete", i, err)
		}
	}
}

// TestDrainContextBoundsOnlyTheCaller: a caller may stop waiting; the drain
// carries on, because the work it is protecting is not the caller's to abandon.
func TestDrainContextBoundsOnlyTheCaller(t *testing.T) {
	s, _ := newTestServer(t, 5*time.Second)

	if !s.gate.enter() {
		t.Fatal("gate should admit before the drain")
	}

	released := make(chan struct{})

	go func() {
		defer s.gate.leave()

		<-released
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if err := s.Drain(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Drain error = %v, want DeadlineExceeded", err)
	}

	if s.hasDrained() {
		t.Fatal("the drain finished when only the caller's wait expired")
	}

	close(released)

	if err := s.Drain(context.Background()); err != nil {
		t.Fatalf("second Drain: %v", err)
	}
}

func TestStartAfterDrainIsRejected(t *testing.T) {
	s, _ := newTestServer(t, time.Second)

	if err := s.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	if err := s.Start(context.Background()); !errors.Is(err, ErrServerStopped) {
		t.Fatalf("Start after Drain = %v, want ErrServerStopped", err)
	}
}

// TestDrainEndsABlockedStart pins the contract the removed Stop only ever
// claimed: a drain ends the serving lifetime rather than merely waiting on it,
// so a Start blocked on its context returns.
func TestDrainEndsABlockedStart(t *testing.T) {
	s, _ := newTestServer(t, time.Second)

	// Stand in for Start's blocking half without a broker: Start waits on the
	// serving context, which is what Stop cancels.
	serveDone := make(chan struct{})

	s.mu.Lock()
	stopServing := s.stopServing
	s.mu.Unlock()

	serveCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.mu.Lock()
	s.stopServing = func() {
		stopServing()
		cancel()
	}
	s.mu.Unlock()

	go func() {
		<-serveCtx.Done()
		close(serveDone)
	}()

	if err := s.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	select {
	case <-serveDone:
	case <-time.After(time.Second):
		t.Fatal("Drain did not end the serving lifetime")
	}
}

// TestDrainPhasesSplitTheBudget pins that WithDrainTimeout is a total: the
// unwind wait comes out of it rather than being added to it, so an embedder
// sizing the budget against its own shutdown never overshoots.
func TestDrainPhasesSplitTheBudget(t *testing.T) {
	cases := []struct {
		budget           time.Duration
		graceful, unwind time.Duration
	}{
		{30 * time.Second, 24 * time.Second, 6 * time.Second},
		{10 * time.Second, 8 * time.Second, 2 * time.Second},
		{0, 0, drainCancelGrace},
		{-time.Second, 0, drainCancelGrace},
	}

	for _, tc := range cases {
		graceful, unwind := drainPhases(tc.budget)
		if graceful != tc.graceful || unwind != tc.unwind {
			t.Errorf("drainPhases(%s) = (%s, %s), want (%s, %s)",
				tc.budget, graceful, unwind, tc.graceful, tc.unwind)
		}

		if tc.budget > 0 && graceful+unwind != tc.budget {
			t.Errorf("drainPhases(%s) sums to %s; the budget must be a total", tc.budget, graceful+unwind)
		}
	}
}

// TestStartDuringDrainIsRejected pins the window hasDrained cannot see: a drain
// that has begun — and so has already read the handles it will stop — but not
// yet finished. A Start that registered in that window would publish a service
// nothing is left to stop: a queue-group member refusing every prompt with 503
// for as long as the connection lives.
func TestStartDuringDrainIsRejected(t *testing.T) {
	s, _ := newTestServer(t, time.Second)

	// Stand in for runDrain having taken its snapshot of the handles.
	s.mu.Lock()
	s.draining = true
	s.mu.Unlock()

	// No broker behind s: a Start that got past the check would panic in
	// AddService rather than return.
	if err := s.Start(context.Background()); !errors.Is(err, ErrServerStopped) {
		t.Fatalf("Start during drain = %v, want ErrServerStopped", err)
	}
}
