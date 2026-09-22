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
	"testing"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	natsagent "github.com/henomis/phero/nats"
)

// echoHandler is a Handler with no LLM behind it: this test is about when the
// server becomes callable, not about what it answers.
type echoHandler struct{ reply string }

func (e echoHandler) Run(_ context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	return &agent.Result{Parts: []llm.ContentPart{llm.Text(e.reply)}}, nil
}

// TestNATS_WaitReady_AgentIsCallableImmediately: an agent started for one unit
// of work is prompted as soon as WaitReady returns — no sleep, no polling
// discovery until the name shows up.
func TestNATS_WaitReady_AgentIsCallableImmediately(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)

	srv, err := natsagent.New(nc, echoHandler{reply: "ready"}, "ready-owner", "probe",
		natsagent.WithSession("job-1"),
		natsagent.WithHeartbeatInterval(5*time.Second),
		natsagent.WithKeepaliveInterval(5*time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	t.Cleanup(srvCancel)

	go func() { _ = srv.Start(srvCtx) }()

	if err = srv.WaitReady(ctx); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	client := natsagent.NewClient(nc)

	handles, err := client.Discover(ctx, natsagent.FilterBySession("job-1"))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if len(handles) != 1 {
		t.Fatalf("discovered %d agents right after WaitReady, want 1", len(handles))
	}

	stream, err := handles[0].Prompt(ctx, "hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	defer stream.Close()

	reply, err := stream.Text(ctx)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}

	if reply != "ready" {
		t.Errorf("reply = %q, want %q", reply, "ready")
	}
}

// A server that is shut down before it serves must wake its waiters rather than
// leave them to their own timeout.
func TestNATS_WaitReady_StoppedServer(t *testing.T) {
	nc := requireNATS(t)

	srv, err := natsagent.New(nc, echoHandler{reply: "unused"}, "ready-owner", "never-started")
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	if drainErr := srv.Drain(context.Background()); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err = srv.WaitReady(ctx); err == nil {
		t.Fatal("WaitReady must report that the server will never serve")
	}
}
