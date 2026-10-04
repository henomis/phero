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

	"golang.org/x/sync/singleflight"

	"github.com/henomis/phero/v2/llm"
)

const (
	// defaultResolverCacheTTL bounds how long a resolved handle is reused before
	// being re-discovered, so endpoint metadata (max_payload, attachments_ok)
	// cannot drift unbounded across a redeploy. Liveness is enforced separately
	// and continuously, by heartbeats.
	defaultResolverCacheTTL = 5 * time.Minute

	// defaultReadyPoll is the [Resolver.WaitReady] re-check interval when none is
	// given. It costs no NATS traffic — it re-reads the heartbeat tracker.
	defaultReadyPoll = 50 * time.Millisecond
)

// AgentKey identifies an agent by the owner it registers under and its instance
// name — the name as registered, so [InstanceName] for a sessioned agent.
type AgentKey struct {
	Owner string
	Name  string
}

func (k AgentKey) String() string { return k.Owner + "/" + k.Name }

// discoverFunc is the discovery seam, so the resolution logic can be exercised
// against edge results without a broker.
type discoverFunc func(context.Context, ...DiscoverOption) ([]*AgentHandle, error)

// ResolverOption configures a [Resolver].
type ResolverOption func(*resolverConfig)

type resolverConfig struct {
	cacheTTL time.Duration
}

// WithResolverCacheTTL bounds how long a resolved handle is reused before it is
// discovered again. Default 5 minutes; a non-positive value selects the default.
//
// This is not the liveness check — that is continuous, from heartbeats. The TTL
// exists so a handle's *metadata* cannot go stale indefinitely across a
// redeploy that keeps the same name.
func WithResolverCacheTTL(d time.Duration) ResolverOption {
	return func(c *resolverConfig) {
		if d > 0 {
			c.cacheTTL = d
		}
	}
}

// Resolver turns an agent's (owner, name) identity into a live [AgentHandle]
// without paying discovery on every call.
//
// It joins the two halves phero already has and leaves unconnected: [Client]
// finds agents, and [HeartbeatTracker] says which are alive. Caching a handle is
// only safe because the tracker keeps answering the second question — a cache
// hit is served while the agent is still beating, and a discovery stall is paid
// once per agent rather than once per prompt.
//
// Three behaviours are worth knowing, because each exists for a failure that is
// easy to meet and hard to diagnose:
//
//   - Concurrent misses for one agent collapse into a single discovery. Without
//     that, a caller with a worker pool fans out one account-wide broadcast per
//     worker on a cold start, a TTL expiry, or an invalidation burst — and an
//     invalidation burst is what a transient fault produces, so the stampede
//     arrives exactly when the fleet is already degraded.
//   - [Resolver.Invalidate] drops a handle so the next resolve re-discovers,
//     which is what lets a target restart between calls.
//   - An empty-but-nil discovery result is an error, never an index into an
//     empty slice. Callers run this on worker goroutines that often have no
//     panic recovery.
//
// It is safe for concurrent use.
type Resolver struct {
	discover discoverFunc
	tracker  *HeartbeatTracker
	ttl      time.Duration

	mu    sync.RWMutex
	cache map[string]resolverEntry

	inflight singleflight.Group
}

type resolverEntry struct {
	handle *AgentHandle
	expiry time.Time
}

// NewResolver builds a Resolver over c and starts a [HeartbeatTracker], so
// liveness is tracked from the moment it returns — create it *before* starting
// the agents whose first beats it must observe, or [Resolver.WaitReady] will
// wait for beats it already missed.
//
// Discovery timeouts come from c; see [WithDiscoveryTimeout]. Call
// [Resolver.Close] to release the heartbeat subscription.
func NewResolver(c *Client, opts ...ResolverOption) (*Resolver, error) {
	if c == nil {
		return nil, ErrNilClient
	}

	cfg := &resolverConfig{cacheTTL: defaultResolverCacheTTL}

	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	tracker, err := NewHeartbeatTracker(c.nc)
	if err != nil {
		return nil, fmt.Errorf("nats: resolver heartbeat tracker: %w", err)
	}

	return &Resolver{
		discover: c.Discover,
		tracker:  tracker,
		ttl:      cfg.cacheTTL,
		cache:    make(map[string]resolverEntry),
	}, nil
}

// Resolve returns a handle for the agent at (owner, name).
//
// A cached handle is reused while it is unexpired *and* the agent is still
// beating; otherwise the agent is discovered and the result cached. A discovery
// failure — including nobody answering — is returned for the caller to
// classify, since only the caller knows whether that is worth retrying.
func (r *Resolver) Resolve(ctx context.Context, owner, name string) (*AgentHandle, error) {
	key := AgentKey{Owner: owner, Name: name}.String()

	r.mu.RLock()
	e, ok := r.cache[key]
	r.mu.RUnlock()

	if ok && time.Now().Before(e.expiry) && r.tracker.IsAgentOnline(owner, name) {
		return e.handle, nil
	}

	// One discovery per agent, however many callers missed at once. The losers
	// wait for the winner's result instead of each broadcasting.
	//
	// That discovery is shared, so it must not run on the lifetime of whichever
	// caller happened to start it: if it did, that caller cancelling would fail
	// every other caller waiting on it. WithoutCancel keeps ctx's values and
	// drops its cancellation and deadline; Discover is still bounded by the
	// client's discovery timeout, so nothing runs unbounded. Each caller then
	// waits on its own ctx instead.
	//
	// Detaching means a dead ctx no longer stops Discover at its first check, so
	// check it here: a caller that is already gone must not start a broadcast.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	shared := context.WithoutCancel(ctx)

	ch := r.inflight.DoChan(key, func() (any, error) {
		return r.discoverAndCache(shared, key, owner, name)
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}

		handle, isHandle := res.Val.(*AgentHandle)
		if !isHandle || handle == nil {
			return nil, fmt.Errorf("nats: resolve %s: no agent responded", key)
		}

		return handle, nil
	case <-ctx.Done():
		// Only this caller gives up. The discovery carries on for the others,
		// and its result still lands in the cache for the next Resolve.
		return nil, ctx.Err()
	}
}

// discoverAndCache performs one discovery and stores the result. It runs inside
// the singleflight group, so exactly one caller per key executes it, and on a
// context no single caller can cancel — it may finish after every caller has
// given up, in which case it still caches.
func (r *Resolver) discoverAndCache(ctx context.Context, key, owner, name string) (*AgentHandle, error) {
	handles, err := r.discover(ctx, FilterByOwner(owner), FilterByName(name))
	if err != nil {
		return nil, err
	}

	// Discover returns ErrNoAgentsFound for an empty result today. Guard anyway:
	// this often runs on a worker goroutine with no panic recovery, and a
	// contract drift must surface as an error rather than an index into nothing.
	if len(handles) == 0 {
		return nil, fmt.Errorf("nats: resolve %s: no agent responded", key)
	}

	h := handles[0]

	r.mu.Lock()
	r.cache[key] = resolverEntry{handle: h, expiry: time.Now().Add(r.ttl)}
	r.mu.Unlock()

	return h, nil
}

// Invalidate drops any cached handle for (owner, name), so the next
// [Resolver.Resolve] discovers again.
//
// Call it when a prompt fails in a way that might mean the agent moved,
// restarted or went away. It is the counterpart to caching: without it a handle
// survives its agent.
func (r *Resolver) Invalidate(owner, name string) {
	key := AgentKey{Owner: owner, Name: name}.String()

	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()
}

// AsTool wraps a remote agent as an [llm.Tool] that any local agent can call,
// resolving it on every call rather than capturing a snapshot.
//
// This is the difference from [Client.AsTool], which binds one *AgentInfo for
// the life of the tool: a target that restarts is never re-discovered, so the
// tool keeps failing against a handle that is gone. Here each call resolves
// (from cache while the agent is beating) and a failure invalidates, so the next
// call finds the new instance.
func (r *Resolver) AsTool(owner, name, toolName, toolDesc string) (*llm.Tool, error) {
	type input struct {
		Prompt string `json:"prompt" jsonschema:"The prompt text to send to the remote agent."`
	}

	return llm.NewTool(toolName, toolDesc,
		func(ctx context.Context, args input) (string, error) {
			handle, err := r.Resolve(ctx, owner, name)
			if err != nil {
				return "", err
			}

			stream, err := handle.Prompt(ctx, args.Prompt)
			if err != nil {
				r.Invalidate(owner, name)
				return "", err
			}
			defer stream.Close()

			text, err := stream.Text(ctx)
			if err != nil {
				r.Invalidate(owner, name)
				return "", err
			}

			return text, nil
		},
	)
}

// WaitReady blocks until every key has been heard from — a recent heartbeat — or
// timeout elapses. On timeout it reports which agents never beat.
//
// It answers startup readiness from beats the tracker is already receiving, so
// it costs no NATS traffic, unlike polling discovery. poll is the re-check
// interval; a non-positive value selects a default.
func (r *Resolver) WaitReady(ctx context.Context, keys []AgentKey, timeout, poll time.Duration) error {
	if poll <= 0 {
		poll = defaultReadyPoll
	}

	deadline := time.Now().Add(timeout)

	for {
		missing := r.missing(keys)
		if len(missing) == 0 {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("nats: timeout waiting for agents to become discoverable: %v", missing)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// Close stops the heartbeat tracker. The [Client] it was built over is not
// affected — the Resolver does not own it.
func (r *Resolver) Close() error {
	return r.tracker.Stop()
}

// missing returns the string form of every key whose agent is not currently
// beating.
func (r *Resolver) missing(keys []AgentKey) []string {
	var out []string

	for _, k := range keys {
		if !r.tracker.IsAgentOnline(k.Owner, k.Name) {
			out = append(out, k.String())
		}
	}

	return out
}
