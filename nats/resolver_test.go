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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestResolver builds a Resolver with no NATS behind it: the heartbeat
// tracker is hand-made (as in heartbeat_test.go) and discovery is a stub, so the
// resolution logic can be exercised against edge results a broker would not
// easily produce.
func newTestResolver(t *testing.T, discover discoverFunc) *Resolver {
	t.Helper()

	r := &Resolver{
		discover: discover,
		tracker:  newTestTracker(),
		ttl:      time.Minute,
		cache:    make(map[string]resolverEntry),
	}

	t.Cleanup(func() { _ = r.Close() })

	return r
}

// testOwner is the owner every resolver test registers its agents under.
const testOwner = "o"

// online marks (testOwner, name) as currently beating.
func online(r *Resolver, name string) {
	seed(r, name, time.Second)
}

// offline marks (testOwner, name) as having last beaten too long ago.
func offline(r *Resolver, name string) {
	seed(r, name, time.Minute)
}

func seed(r *Resolver, name string, age time.Duration) {
	r.tracker.mu.Lock()
	r.tracker.byAgent[agentKey(testOwner, name)] = beat(10, age)
	r.tracker.mu.Unlock()
}

func TestResolverCachesWhileTheAgentBeats(t *testing.T) {
	var discoveries atomic.Int64

	handles := []*AgentHandle{{AgentInfo: AgentInfo{Owner: "o", Name: "worker"}}}

	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		discoveries.Add(1)
		return handles, nil
	})

	online(r, "worker")

	first, err := r.Resolve(t.Context(), "o", "worker")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	second, err := r.Resolve(t.Context(), "o", "worker")
	if err != nil {
		t.Fatalf("Resolve (cached): %v", err)
	}

	if first != second {
		t.Error("a second resolve re-discovered instead of reusing the cached handle")
	}

	if n := discoveries.Load(); n != 1 {
		t.Errorf("%d discoveries for two resolves; want 1", n)
	}
}

// A cached handle must not be served for an agent that has stopped beating.
// Caching is only safe *because* liveness is tracked separately; without this
// check the resolver would keep prompting an agent that is gone until something
// else timed out.
func TestResolverDoesNotServeADeadAgentFromCache(t *testing.T) {
	var discoveries atomic.Int64

	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		discoveries.Add(1)
		return []*AgentHandle{{AgentInfo: AgentInfo{Owner: "o", Name: "worker"}}}, nil
	})

	online(r, "worker")

	if _, err := r.Resolve(t.Context(), "o", "worker"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// The entry is still well within its TTL; only the beats have stopped.
	offline(r, "worker")

	if _, err := r.Resolve(t.Context(), "o", "worker"); err != nil {
		t.Fatalf("Resolve (stale): %v", err)
	}

	if n := discoveries.Load(); n != 2 {
		t.Errorf("%d discoveries; a handle whose agent stopped beating must be re-discovered", n)
	}
}

// An expired entry must be re-discovered even while the agent keeps beating:
// the TTL is about the handle's metadata going stale, not about liveness.
func TestResolverExpiresOnTTL(t *testing.T) {
	var discoveries atomic.Int64

	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		discoveries.Add(1)
		return []*AgentHandle{{AgentInfo: AgentInfo{Owner: "o", Name: "worker"}}}, nil
	})
	r.ttl = time.Nanosecond

	online(r, "worker")

	for range 2 {
		if _, err := r.Resolve(t.Context(), "o", "worker"); err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		time.Sleep(time.Millisecond)
	}

	if n := discoveries.Load(); n != 2 {
		t.Errorf("%d discoveries; an expired entry must be re-discovered", n)
	}
}

func TestResolverInvalidateForcesRediscovery(t *testing.T) {
	var discoveries atomic.Int64

	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		discoveries.Add(1)
		// A fresh handle each time, as a restarted agent would produce.
		return []*AgentHandle{{AgentInfo: AgentInfo{Owner: "o", Name: "worker"}}}, nil
	})

	online(r, "worker")

	first, err := r.Resolve(t.Context(), "o", "worker")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	r.Invalidate("o", "worker")

	second, err := r.Resolve(t.Context(), "o", "worker")
	if err != nil {
		t.Fatalf("Resolve (post-invalidate): %v", err)
	}

	if first == second {
		t.Error("Invalidate did not force a re-discovery")
	}

	if n := discoveries.Load(); n != 2 {
		t.Errorf("%d discoveries; want 2", n)
	}
}

// A nil-error empty result must become an error, never an index into an empty
// slice: this runs on caller worker goroutines that often have no panic
// recovery, so a contract drift must degrade rather than crash the process.
func TestResolverEmptyDiscoveryIsAnErrorNotAPanic(t *testing.T) {
	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		return nil, nil
	})

	_, err := r.Resolve(t.Context(), "o", "worker")
	if err == nil {
		t.Fatal("an empty discovery result must be an error")
	}

	if !strings.Contains(err.Error(), "o/worker") {
		t.Errorf("error %q should name the unresolved agent", err)
	}
}

func TestResolverPropagatesDiscoveryError(t *testing.T) {
	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		return nil, ErrNoAgentsFound
	})

	if _, err := r.Resolve(t.Context(), "o", "worker"); !errors.Is(err, ErrNoAgentsFound) {
		t.Fatalf("Resolve = %v, want ErrNoAgentsFound", err)
	}
}

// Concurrent misses for one agent must collapse into a single discovery.
//
// Without it, a caller with a worker pool fans out one account-wide
// $SRV.INFO.agents broadcast per worker on a cold start, a TTL expiry or an
// invalidation burst — each blocking for the full discovery stall window. An
// invalidation burst is what a transient fault produces, so the stampede
// arrives exactly when the fleet is already degraded.
func TestResolverCollapsesConcurrentMisses(t *testing.T) {
	const callers = 64

	var discoveries atomic.Int64

	release := make(chan struct{})
	handle := &AgentHandle{AgentInfo: AgentInfo{Owner: "o", Name: "worker"}}

	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		discoveries.Add(1)
		<-release // hold every caller inside the discovery so they genuinely overlap

		return []*AgentHandle{handle}, nil
	})

	var wg sync.WaitGroup

	results := make([]*AgentHandle, callers)
	errs := make([]error, callers)

	for i := range callers {
		wg.Go(func() { results[i], errs[i] = r.Resolve(context.Background(), "o", "worker") })
	}

	// Give the callers time to pile up on the same key, then let discovery finish.
	time.Sleep(200 * time.Millisecond)
	close(release)
	wg.Wait()

	if n := discoveries.Load(); n != 1 {
		t.Errorf("%d callers caused %d discoveries; want exactly 1", callers, n)
	}

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}

		if results[i] != handle {
			t.Fatalf("caller %d got a handle other than the one discovered", i)
		}
	}
}

func TestResolverWaitReadyReportsWhoNeverBeat(t *testing.T) {
	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		return nil, nil
	})

	online(r, "present")

	keys := []AgentKey{{Owner: "o", Name: "present"}, {Owner: "o", Name: "ghost"}}

	err := r.WaitReady(t.Context(), keys, 50*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("WaitReady must fail while an agent has never beaten")
	}

	if !strings.Contains(err.Error(), "o/ghost") {
		t.Errorf("error %q should name the missing agent", err)
	}

	if strings.Contains(err.Error(), "o/present") {
		t.Errorf("error %q should not name an agent that is beating", err)
	}
}

func TestResolverWaitReadyReturnsWhenAllBeat(t *testing.T) {
	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		return nil, nil
	})

	online(r, "a")
	online(r, "b")

	keys := []AgentKey{{Owner: "o", Name: "a"}, {Owner: "o", Name: "b"}}

	if err := r.WaitReady(t.Context(), keys, time.Second, time.Millisecond); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
}

func TestResolverWaitReadyHonoursContext(t *testing.T) {
	r := newTestResolver(t, func(context.Context, ...DiscoverOption) ([]*AgentHandle, error) {
		return nil, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.WaitReady(ctx, []AgentKey{{Owner: "o", Name: "ghost"}}, time.Minute, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitReady = %v, want context.Canceled", err)
	}
}

func TestNewResolverRejectsANilClient(t *testing.T) {
	if _, err := NewResolver(nil); !errors.Is(err, ErrNilClient) {
		t.Fatalf("NewResolver(nil) = %v, want ErrNilClient", err)
	}
}

func TestAgentKeyString(t *testing.T) {
	if got := (AgentKey{Owner: "o", Name: "worker-prod"}).String(); got != "o/worker-prod" {
		t.Errorf("AgentKey.String() = %q, want %q", got, "o/worker-prod")
	}
}

// blockingDiscovery is a discovery stub for (testOwner, "worker") that parks until released, honouring
// its context the way Client.Discover does. entered is signalled once per call.
type blockingDiscovery struct {
	calls   atomic.Int64
	entered chan struct{}
	release chan struct{}
	handle  *AgentHandle
}

func newBlockingDiscovery() *blockingDiscovery {
	return &blockingDiscovery{
		entered: make(chan struct{}, 8),
		release: make(chan struct{}),
		handle:  &AgentHandle{AgentInfo: AgentInfo{Owner: testOwner, Name: "worker"}},
	}
}

func (d *blockingDiscovery) discover(ctx context.Context, _ ...DiscoverOption) ([]*AgentHandle, error) {
	d.calls.Add(1)

	d.entered <- struct{}{}

	select {
	case <-d.release:
		return []*AgentHandle{d.handle}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestResolverOneCallerCancellingDoesNotFailTheOthers pins that a collapsed
// discovery does not run on whichever caller happened to start it: that
// caller's cancellation is its own, and the callers sharing the discovery
// still get the handle.
func TestResolverOneCallerCancellingDoesNotFailTheOthers(t *testing.T) {
	d := newBlockingDiscovery()
	r := newTestResolver(t, d.discover)

	ctxA, cancelA := context.WithCancel(t.Context())
	defer cancelA()

	errA := make(chan error, 1)

	go func() {
		_, err := r.Resolve(ctxA, testOwner, "worker")
		errA <- err
	}()

	// A is now the singleflight leader, parked inside discovery.
	<-d.entered

	type result struct {
		h   *AgentHandle
		err error
	}

	resB := make(chan result, 1)

	go func() {
		h, err := r.Resolve(t.Context(), testOwner, "worker")
		resB <- result{h, err}
	}()

	// Give B time to join the in-flight discovery rather than start its own.
	time.Sleep(50 * time.Millisecond)

	cancelA()

	select {
	case err := <-errA:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled caller got %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled caller did not return")
	}

	close(d.release)

	select {
	case res := <-resB:
		if res.err != nil {
			t.Fatalf("live caller got %v, want the handle", res.err)
		}

		if res.h != d.handle {
			t.Fatalf("live caller got %v, want %v", res.h, d.handle)
		}
	case <-time.After(time.Second):
		t.Fatal("live caller did not return")
	}

	if got := d.calls.Load(); got != 1 {
		t.Fatalf("discovery ran %d times, want 1", got)
	}
}

// TestResolverCancelledCallerReturnsPromptly pins that detaching the shared
// discovery from its leader's lifetime does not make the leader wait for it.
func TestResolverCancelledCallerReturnsPromptly(t *testing.T) {
	d := newBlockingDiscovery()
	defer close(d.release)

	r := newTestResolver(t, d.discover)

	ctx, cancel := context.WithCancel(t.Context())

	errc := make(chan error, 1)

	go func() {
		_, err := r.Resolve(ctx, testOwner, "worker")
		errc <- err
	}()

	<-d.entered
	cancel()

	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Resolve = %v, want context.Canceled", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("a cancelled Resolve waited for the discovery it no longer needs")
	}
}

// TestResolverAbandonedDiscoveryStillFillsTheCache pins the payoff of letting
// the discovery outlive its caller: the next Resolve is served from cache.
func TestResolverAbandonedDiscoveryStillFillsTheCache(t *testing.T) {
	d := newBlockingDiscovery()
	r := newTestResolver(t, d.discover)

	online(r, "worker")

	ctx, cancel := context.WithCancel(t.Context())

	errc := make(chan error, 1)

	go func() {
		_, err := r.Resolve(ctx, testOwner, "worker")
		errc <- err
	}()

	<-d.entered
	cancel()
	<-errc

	// The discovery nobody is waiting for any more completes and caches.
	close(d.release)

	deadline := time.Now().Add(time.Second)

	for {
		r.mu.RLock()
		_, cached := r.cache[AgentKey{Owner: testOwner, Name: "worker"}.String()]
		r.mu.RUnlock()

		if cached {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("the abandoned discovery never reached the cache")
		}

		time.Sleep(5 * time.Millisecond)
	}

	h, err := r.Resolve(t.Context(), testOwner, "worker")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if h != d.handle {
		t.Fatalf("Resolve = %v, want the cached %v", h, d.handle)
	}

	if got := d.calls.Load(); got != 1 {
		t.Fatalf("discovery ran %d times, want 1 (second Resolve should hit the cache)", got)
	}
}

// TestResolverDeadCallerStartsNoDiscovery pins that detaching the discovery
// from its caller did not also detach the check Discover makes first: a caller
// whose context has already ended gets its error without a broadcast.
func TestResolverDeadCallerStartsNoDiscovery(t *testing.T) {
	d := newBlockingDiscovery()
	defer close(d.release)

	r := newTestResolver(t, d.discover)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := r.Resolve(ctx, testOwner, "worker"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve = %v, want context.Canceled", err)
	}

	if got := d.calls.Load(); got != 0 {
		t.Fatalf("discovery ran %d times for a caller that was already gone, want 0", got)
	}
}
