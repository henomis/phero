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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	natsagent "github.com/henomis/phero/nats"
)

// slowHandler stands in for an agent mid-LLM-call: it takes a known amount of
// time and reports whether its context was cancelled underneath it. No LLM is
// needed — nats.Handler is an interface, and the point under test is the
// server's shutdown discipline, not the model.
type slowHandler struct {
	started   chan struct{}
	work      time.Duration
	reply     string
	cancelled bool
}

func (h *slowHandler) Run(ctx context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	close(h.started)

	select {
	case <-time.After(h.work):
	case <-ctx.Done():
		h.cancelled = true
	}

	return &agent.Result{Parts: []llm.ContentPart{llm.Text(h.reply)}}, nil
}

// TestNATSServerDrainsInFlightPrompt is the wire-level proof of the drain: a
// prompt already being served survives the shutdown that begins while it runs,
// and the caller gets its answer rather than a 500.
func TestNATSServerDrainsInFlightPrompt(t *testing.T) {
	nc := requireNATS(t)

	h := &slowHandler{started: make(chan struct{}), work: 1500 * time.Millisecond, reply: "finished"}

	srv, err := natsagent.New(nc, h, "drain-owner", "drain-agent",
		natsagent.WithHeartbeatInterval(time.Second),
		natsagent.WithKeepaliveInterval(time.Second),
		natsagent.WithDrainTimeout(10*time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start(srvCtx) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := natsagent.NewClient(nc, natsagent.WithInactivityTimeout(10*time.Second))

	var handles []*natsagent.AgentHandle

	// Wait for the agent to become discoverable.
	for range 20 {
		handles, err = client.Discover(ctx, natsagent.FilterByOwner("drain-owner"))
		if err == nil && len(handles) > 0 {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if len(handles) == 0 {
		t.Fatalf("agent never became discoverable: %v", err)
	}

	stream, err := handles[0].Prompt(ctx, "hello")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	// Shut the server down while the handler is mid-call. Before the drain
	// existed this cancelled the handler's context in the same instant, and the
	// caller below received a 500 instead of an answer.
	<-h.started
	srvCancel()

	text, err := stream.Text(ctx)
	if err != nil {
		t.Fatalf("Text after shutdown began: %v", err)
	}

	if text != "finished" {
		t.Errorf("reply = %q, want %q", text, "finished")
	}

	if h.cancelled {
		t.Error("the handler was cancelled by a shutdown that had drain budget left")
	}

	select {
	case err := <-startErr:
		if err != nil {
			t.Errorf("Start returned %v, want a clean drain", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("Start did not return after the drain")
	}
}

// TestNATSServerDrainRefusesNewPrompts pins the other half: once the shutdown
// has begun the instance stops taking work, so a caller is free to retry
// somewhere else instead of waiting on a server that is going away.
func TestNATSServerDrainRefusesNewPrompts(t *testing.T) {
	nc := requireNATS(t)

	h := &slowHandler{started: make(chan struct{}), work: 2 * time.Second, reply: "finished"}

	srv, err := natsagent.New(nc, h, "drain-owner-2", "drain-agent-2",
		natsagent.WithHeartbeatInterval(time.Second),
		natsagent.WithDrainTimeout(10*time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()

	go func() { _ = srv.Start(srvCtx) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := natsagent.NewClient(nc, natsagent.WithInactivityTimeout(5*time.Second))

	var handles []*natsagent.AgentHandle

	for range 20 {
		handles, err = client.Discover(ctx, natsagent.FilterByOwner("drain-owner-2"))
		if err == nil && len(handles) > 0 {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if len(handles) == 0 {
		t.Fatalf("agent never became discoverable: %v", err)
	}

	first, err := handles[0].Prompt(ctx, "hello")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer first.Close()

	<-h.started
	srvCancel()

	// A second prompt on the same handle, now that the drain has begun. It is
	// either refused outright or never answered (the endpoint is gone); what it
	// must not do is get served by a server that is shutting down.
	second, err := handles[0].Prompt(ctx, "hello again")
	if err != nil {
		return // rejected before publish — also acceptable
	}
	defer second.Close()

	if text, textErr := second.Text(ctx); textErr == nil && text == "finished" {
		t.Error("a prompt was served after the drain began")
	}

	// The first prompt still completes.
	if text, textErr := first.Text(ctx); textErr != nil || text != "finished" {
		t.Errorf("in-flight prompt = (%q, %v), want (%q, nil)", text, textErr, "finished")
	}
}

// TestNATSServerDrainRacingStartLeavesNothingRegistered races Drain against a
// Start that is still registering. A drain that began before Start published
// its handles used to miss them: Start then registered a service nothing would
// ever stop — a queue-group member answering every prompt with 503 — and, when
// it also missed the serving context, blocked until its own context ended.
//
// The window is narrow and timing-dependent, so the test sweeps Drain across a
// range of offsets from Start. Start's context is never cancelled: only the
// drain can make it return.
func TestNATSServerDrainRacingStartLeavesNothingRegistered(t *testing.T) {
	nc := requireNATS(t)

	const (
		owner      = "drain-race-owner"
		iterations = 100
	)

	baseline := nc.NumSubscriptions()

	for i := range iterations {
		h := &slowHandler{started: make(chan struct{}), reply: "unused"}

		srv, err := natsagent.New(nc, h, owner, fmt.Sprintf("agent-%d", i),
			natsagent.WithHeartbeatInterval(time.Second),
			natsagent.WithDrainTimeout(time.Second),
		)
		if err != nil {
			t.Fatalf("natsagent.New: %v", err)
		}

		startErr := make(chan error, 1)
		go func() { startErr <- srv.Start(context.Background()) }()

		// Four sweeps of 0, 10µs … 240µs. The window sits in the first few
		// hundred microseconds of Start, while it registers; later offsets are
		// an ordinary drain of a registered server, covered above.
		time.Sleep(time.Duration(i%25) * 10 * time.Microsecond)

		if err := srv.Drain(context.Background()); err != nil {
			t.Fatalf("iteration %d: Drain: %v", i, err)
		}

		select {
		case err := <-startErr:
			if err != nil && !errors.Is(err, natsagent.ErrServerStopped) {
				t.Fatalf("iteration %d: Start = %v, want nil or ErrServerStopped", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("iteration %d: Start still blocked after Drain returned", i)
		}
	}

	// Discovery only sees the $SRV verb subscriptions. An endpoint subscribed on
	// a service that had already stopped answers no discovery but still sits in
	// the queue group, so count subscriptions too. Stop drains asynchronously,
	// hence the settle.
	deadline := time.Now().Add(5 * time.Second)
	for nc.NumSubscriptions() > baseline && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if got := nc.NumSubscriptions(); got != baseline {
		t.Fatalf("after every drain, %d subscription(s) remain on the connection, want %d", got, baseline)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	handles, err := natsagent.NewClient(nc).Discover(ctx, natsagent.FilterByOwner(owner))
	if !errors.Is(err, natsagent.ErrNoAgentsFound) {
		names := make([]string, 0, len(handles))
		for _, h := range handles {
			names = append(names, h.Name)
		}

		t.Fatalf("after every drain, discovery = (%v, %v), want ErrNoAgentsFound", names, err)
	}
}
