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
	"testing"
	"time"

	natsio "github.com/nats-io/nats.go"

	natsagent "github.com/henomis/phero/nats"
)

// TestNATSServerReadyMeansReachable is the wire-level contract of Ready: the
// moment it closes, another connection can discover and prompt the agent, and
// has already been sent its first heartbeat. The heartbeat interval is far
// longer than the test, so the beat that arrives can only be the first one.
func TestNATSServerReadyMeansReachable(t *testing.T) {
	nc := requireNATS(t)

	other, err := natsio.Connect(natsURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(other.Close)

	beats, err := other.SubscribeSync("agents.hb.*.ready-owner.ready-agent")
	if err != nil {
		t.Fatalf("subscribe heartbeats: %v", err)
	}
	if err = other.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	srv, err := natsagent.New(nc, staticHandler{reply: "ready"}, "ready-owner", "ready-agent",
		natsagent.WithHeartbeatInterval(time.Minute),
		natsagent.WithDrainTimeout(time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()

	startErr := make(chan error, 1)
	go func() { startErr <- srv.Start(srvCtx) }()

	select {
	case <-srv.Ready():
	case err = <-startErr:
		t.Fatalf("Start returned before Ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Ready did not close")
	}

	if _, err = beats.NextMsg(2 * time.Second); err != nil {
		t.Fatalf("first heartbeat: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agents, err := natsagent.NewClient(other).Discover(ctx,
		natsagent.FilterByOwner("ready-owner"), natsagent.FilterByName("ready-agent"))
	if err != nil || len(agents) != 1 {
		t.Fatalf("Discover = %d agents, %v; want 1", len(agents), err)
	}

	stream, err := agents[0].Prompt(ctx, "hi")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	if text, textErr := stream.Text(ctx); textErr != nil || text != "ready" {
		t.Fatalf("Text = %q, %v; want %q", text, textErr, "ready")
	}

	srvCancel()

	if err = <-startErr; err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// TestNATSServerReadyStaysOpenWhenRegistrationFails pins the other half: a
// Start that never registered returns its error and leaves Ready open, so a
// caller selecting on both gets the error rather than a false "ready".
func TestNATSServerReadyStaysOpenWhenRegistrationFails(t *testing.T) {
	requireNATS(t)

	nc, err := natsio.Connect(natsURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	srv, err := natsagent.New(nc, staticHandler{reply: "never"}, "ready-owner", "closed-agent")
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	nc.Close()

	if err = srv.Start(context.Background()); err == nil {
		t.Fatal("Start on a closed connection succeeded")
	}

	select {
	case <-srv.Ready():
		t.Fatal("Ready closed although Start failed")
	default:
	}

	if errors.Is(err, natsagent.ErrServerStopped) {
		t.Fatalf("Start = %v, want a registration error", err)
	}
}
