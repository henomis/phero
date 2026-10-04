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

//go:build e2e

package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"

	natsagent "github.com/henomis/phero/v2/nats"
)

// startResolvableAgent serves staticHandler under (owner, name) and returns a
// cancel that shuts it down.
func startResolvableAgent(t *testing.T, nc *natsio.Conn, owner, name, reply string) context.CancelFunc {
	t.Helper()

	srv, err := natsagent.New(nc, staticHandler{reply: reply}, owner, name,
		natsagent.WithHeartbeatInterval(200*time.Millisecond),
		natsagent.WithDrainTimeout(time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New(%s/%s): %v", owner, name, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = srv.Start(ctx) }()

	return cancel
}

// TestResolverWaitReadyAndResolve drives the resolver the way an embedder does
// at startup: subscribe to heartbeats first, start the agents, wait for their
// beats rather than polling discovery, then resolve and prompt.
func TestResolverWaitReadyAndResolve(t *testing.T) {
	nc := requireNATS(t)

	// Built before the agent starts, so the tracker sees its first beat.
	resolver, err := natsagent.NewResolver(
		natsagent.NewClient(nc, natsagent.WithDiscoveryTimeout(2*time.Second)),
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	t.Cleanup(func() { _ = resolver.Close() })

	startResolvableAgent(t, nc, "res-owner", "worker", "resolved")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	keys := []natsagent.AgentKey{{Owner: "res-owner", Name: "worker"}}
	if err = resolver.WaitReady(ctx, keys, 15*time.Second, 20*time.Millisecond); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	first, err := resolver.Resolve(ctx, "res-owner", "worker")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	second, err := resolver.Resolve(ctx, "res-owner", "worker")
	if err != nil {
		t.Fatalf("Resolve (cached): %v", err)
	}

	if first != second {
		t.Error("the second resolve paid a discovery instead of using the cache")
	}

	stream, err := first.Prompt(ctx, "hello")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	text, err := stream.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	if text != "resolved" {
		t.Errorf("reply = %q, want %q", text, "resolved")
	}
}

// TestResolverWaitReadyNamesAnAgentThatNeverStarts: readiness must fail with the
// agent named, not merely time out.
func TestResolverWaitReadyNamesAMissingAgent(t *testing.T) {
	nc := requireNATS(t)

	resolver, err := natsagent.NewResolver(natsagent.NewClient(nc))
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	t.Cleanup(func() { _ = resolver.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	keys := []natsagent.AgentKey{{Owner: "res-owner", Name: "ghost"}}

	err = resolver.WaitReady(ctx, keys, 300*time.Millisecond, 20*time.Millisecond)
	if err == nil {
		t.Fatal("WaitReady must fail for an agent that never beats")
	}

	if want := "res-owner/ghost"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q should name the missing agent %q", err, want)
	}
}

// TestResolverAsToolSurvivesARestart is M17: a remote-agent tool built over the
// resolver re-resolves, so a target that restarts between calls is found again.
//
// Client.AsTool cannot do this — it captures one *AgentInfo for the life of the
// tool, so once the instance behind it is gone every later call fails against a
// handle that no longer exists.
func TestResolverAsToolSurvivesARestart(t *testing.T) {
	nc := requireNATS(t)

	resolver, err := natsagent.NewResolver(
		natsagent.NewClient(nc, natsagent.WithDiscoveryTimeout(2*time.Second)),
	)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	t.Cleanup(func() { _ = resolver.Close() })

	stop := startResolvableAgent(t, nc, "restart-owner", "worker", "first instance")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	keys := []natsagent.AgentKey{{Owner: "restart-owner", Name: "worker"}}
	if err = resolver.WaitReady(ctx, keys, 15*time.Second, 20*time.Millisecond); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	tool, err := resolver.AsTool("restart-owner", "worker", "delegate", "Ask the worker.")
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}

	out, err := tool.Handle(ctx, `{"prompt":"who are you?"}`)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	if text, _ := out.(string); text != "first instance" {
		t.Fatalf("first call returned %q, want %q", text, "first instance")
	}

	// Take the agent away and bring a different instance back under the same
	// name — the redeploy the snapshot form cannot survive.
	stop()

	// Wait past the staleness window before the replacement exists, so WaitReady
	// below waits for the *new* instance's beats rather than returning on the old
	// one's. §8.2 makes an agent stale after 3× its advertised interval_s, and
	// interval_s floors at 1 second, so the window is 3s however fast it beats.
	//
	// This is not a wrinkle in the test: it is the liveness contract. Within that
	// window a resolver legitimately still reports the departed instance online
	// and serves its cached handle — the prompt fails, invalidates, and the next
	// call re-discovers. Waiting it out is what makes the assertion below about
	// re-resolution rather than about retry timing.
	time.Sleep(3500 * time.Millisecond)

	startResolvableAgent(t, nc, "restart-owner", "worker", "second instance")

	if err = resolver.WaitReady(ctx, keys, 15*time.Second, 20*time.Millisecond); err != nil {
		t.Fatalf("WaitReady after restart: %v", err)
	}

	// No explicit Invalidate: the tool must recover on its own, because in
	// production nobody is watching for the restart.
	out, err = tool.Handle(ctx, `{"prompt":"who are you now?"}`)
	if err != nil {
		t.Fatalf("call after restart: %v", err)
	}

	if text, _ := out.(string); text != "second instance" {
		t.Errorf("call after restart returned %q, want %q", text, "second instance")
	}
}
