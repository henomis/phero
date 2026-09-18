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

// staticHandler answers immediately; these tests are about registration
// metadata, not about the model.
type staticHandler struct{ reply string }

func (h staticHandler) Run(context.Context, ...llm.ContentPart) (*agent.Result, error) {
	return &agent.Result{Parts: []llm.ContentPart{llm.Text(h.reply)}}, nil
}

// TestSessionIsAdvertisedAsTheSession is the wire-level form of the fix: an
// agent served WithSession registers under the suffixed instance name and
// advertises the *session* as metadata.session. It used to advertise the
// instance name in that field, so every agent reported a distinct "session" and
// nothing could be grouped or filtered by one.
func TestSessionIsAdvertisedAsTheSession(t *testing.T) {
	nc := requireNATS(t)

	srv, err := natsagent.New(nc, staticHandler{reply: "ok"}, "session-owner", "worker",
		natsagent.WithSession("prod"),
		natsagent.WithHeartbeatInterval(time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	defer srvCancel()

	go func() { _ = srv.Start(srvCtx) }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := natsagent.NewClient(nc)

	var handles []*natsagent.AgentHandle

	for range 20 {
		handles, err = client.Discover(ctx, natsagent.FilterByOwner("session-owner"))
		if err == nil && len(handles) > 0 {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if len(handles) == 0 {
		t.Fatalf("agent never became discoverable: %v", err)
	}

	got := handles[0]

	if got.Session != "prod" {
		t.Errorf("advertised session = %q, want %q — the instance name must not occupy this field", got.Session, "prod")
	}

	if want := natsagent.InstanceName("worker", "prod"); got.Name != want {
		t.Errorf("instance name = %q, want %q", got.Name, want)
	}
}

// TestFilterBySessionSelectsOneDeployment: two agents of the same name under
// different sessions, addressed without composing a name.
func TestFilterBySessionSelectsOneDeployment(t *testing.T) {
	nc := requireNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, session := range []string{"blue", "green"} {
		srv, err := natsagent.New(nc, staticHandler{reply: session}, "twin-owner", "twin",
			natsagent.WithSession(session),
			natsagent.WithHeartbeatInterval(time.Second),
		)
		if err != nil {
			t.Fatalf("natsagent.New(%s): %v", session, err)
		}

		srvCtx, srvCancel := context.WithCancel(context.Background())
		defer srvCancel()

		go func() { _ = srv.Start(srvCtx) }()
	}

	client := natsagent.NewClient(nc)

	var (
		handles []*natsagent.AgentHandle
		err     error
	)

	// Wait until both replicas answer, so a single match cannot pass by being early.
	for range 20 {
		handles, err = client.Discover(ctx, natsagent.FilterByOwner("twin-owner"))
		if err == nil && len(handles) == 2 {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if len(handles) != 2 {
		t.Fatalf("expected both replicas discoverable, got %d: %v", len(handles), err)
	}

	blue, err := client.Discover(ctx,
		natsagent.FilterByOwner("twin-owner"),
		natsagent.FilterBySession("blue"),
	)
	if err != nil {
		t.Fatalf("Discover(blue): %v", err)
	}

	if len(blue) != 1 {
		t.Fatalf("FilterBySession(blue) returned %d agents, want 1", len(blue))
	}

	if blue[0].Session != "blue" {
		t.Errorf("selected session = %q, want %q", blue[0].Session, "blue")
	}

	reply, err := blue[0].Prompt(ctx, "which one are you?")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer reply.Close()

	text, err := reply.Text(ctx)
	if err != nil {
		t.Fatalf("Text: %v", err)
	}

	if text != "blue" {
		t.Errorf("the blue session answered %q; the filter selected the wrong replica", text)
	}
}
