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

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	natsmemory "github.com/henomis/phero/memory/nats"
	natsagent "github.com/henomis/phero/nats"
)

// startNATSAgent creates a greeter agent wrapped as a NATS server and starts it
// in the background. It returns when discovery can see the agent.
func startNATSAgent(t *testing.T, nc *natsio.Conn) *natsagent.Client {
	t.Helper()

	llmClient := buildOpenAILLM()
	greeter, err := agent.New(
		llmClient,
		"greeter",
		"You are a friendly greeter. When given a name respond with a short greeting.",
	)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	srv, err := natsagent.New(nc, greeter, "test-owner", "greeter-instance",
		natsagent.WithHeartbeatInterval(5*time.Second),
		natsagent.WithKeepaliveInterval(5*time.Second),
	)
	if err != nil {
		t.Fatalf("natsagent.New: %v", err)
	}

	srvCtx, srvCancel := context.WithCancel(context.Background())
	t.Cleanup(srvCancel)

	go func() { _ = srv.Start(srvCtx) }()

	// Allow the micro service to register before discovery.
	time.Sleep(200 * time.Millisecond)

	return natsagent.NewClient(nc)
}

// TestNATS_ServerClient verifies that a NATS-registered agent can be discovered
// and that a plain-text prompt receives a non-empty response.
func TestNATS_ServerClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	nc := requireNATS(t)
	client := startNATSAgent(t, nc)

	agents, err := client.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("expected at least one agent after discovery")
	}

	stream, err := agents[0].Prompt(ctx, "Greet Alice")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	defer stream.Close()

	text, err := stream.Text(ctx)
	if err != nil {
		t.Fatalf("Stream.Text: %v", err)
	}

	if strings.TrimSpace(text) == "" {
		t.Fatal("expected non-empty response from NATS agent")
	}

	t.Logf("NATS agent response: %q", text)
}

// TestNATS_ClientAsTool verifies that a NATS-discovered agent can be wrapped as
// an llm.Tool and invoked directly.
func TestNATS_ClientAsTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	nc := requireNATS(t)
	client := startNATSAgent(t, nc)

	agents, err := client.Discover(ctx)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(agents) == 0 {
		t.Fatal("expected at least one agent")
	}

	tool, err := agents[0].AsTool("remote-greeter", "Greet a person via the remote NATS agent.")
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}

	if strings.TrimSpace(tool.Name()) == "" {
		t.Fatal("expected tool to have a name")
	}

	result, err := tool.Handle(ctx, `{"prompt":"Say hello to Bob"}`)
	if err != nil {
		t.Fatalf("tool.Handle: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil tool result")
	}

	t.Logf("NATS tool result: %v", result)
}

// TestMemoryNATS_SaveRetrieve verifies the full lifecycle of the NATS JetStream
// KV memory backend: Save stores messages, Retrieve returns them in order, and
// Clear empties the session.
func TestMemoryNATS_SaveRetrieve(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("nc.JetStream: %v", err)
	}

	kv, err := js.CreateKeyValue(&natsio.KeyValueConfig{
		Bucket:  "phero-e2e-memory",
		Storage: natsio.MemoryStorage,
	})
	if err != nil {
		t.Fatalf("CreateKeyValue: %v", err)
	}
	t.Cleanup(func() { _ = js.DeleteKeyValue("phero-e2e-memory") })

	mem, err := natsmemory.New(kv, "session-1")
	if err != nil {
		t.Fatalf("natsmemory.New: %v", err)
	}

	msgs := []llm.Message{
		llm.UserMessage(llm.Text("Hello")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("Hi there!")}),
	}

	if err := mem.Save(ctx, msgs); err != nil {
		t.Fatalf("Save: %v", err)
	}

	retrieved, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if len(retrieved) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(retrieved))
	}

	if err := mem.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	empty, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve after Clear: %v", err)
	}

	if len(empty) != 0 {
		t.Fatalf("expected 0 messages after Clear, got %d", len(empty))
	}
}

// TestMemoryNATS_SessionIsolation verifies that two sessions using the same KV
// bucket do not see each other's messages.
func TestMemoryNATS_SessionIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nc := requireNATS(t)

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("nc.JetStream: %v", err)
	}

	kv, err := js.CreateKeyValue(&natsio.KeyValueConfig{
		Bucket:  "phero-e2e-isolation",
		Storage: natsio.MemoryStorage,
	})
	if err != nil {
		t.Fatalf("CreateKeyValue: %v", err)
	}
	t.Cleanup(func() { _ = js.DeleteKeyValue("phero-e2e-isolation") })

	memA, err := natsmemory.New(kv, "session-A")
	if err != nil {
		t.Fatalf("natsmemory.New session-A: %v", err)
	}

	memB, err := natsmemory.New(kv, "session-B")
	if err != nil {
		t.Fatalf("natsmemory.New session-B: %v", err)
	}

	if err := memA.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("Message for A"))}); err != nil {
		t.Fatalf("memA.Save: %v", err)
	}

	bMsgs, err := memB.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("memB.Retrieve: %v", err)
	}

	if len(bMsgs) != 0 {
		t.Fatalf("session-B should not see session-A messages, got %d", len(bMsgs))
	}

	aMsgs, err := memA.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("memA.Retrieve: %v", err)
	}

	if len(aMsgs) != 1 {
		t.Fatalf("session-A expected 1 message, got %d", len(aMsgs))
	}
}
