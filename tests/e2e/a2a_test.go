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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

func newA2ATestServer(t *testing.T) string {
	t.Helper()

	llmClient := buildOpenAILLM()
	greeter, err := agent.New(
		llmClient,
		"greeter",
		"You are a friendly greeter. When given a name, respond with a short greeting containing that name.",
	)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	srv, err := a2a.New(greeter, ts.URL)
	if err != nil {
		t.Fatalf("a2a.New: %v", err)
	}

	mux.Handle("/.well-known/agent-card.json", srv.AgentCardHandler())
	mux.Handle("/", srv.JSONRPCHandler())

	return ts.URL
}

// TestA2AServer_AgentCard verifies that the A2A server exposes the agent card.
func TestA2AServer_AgentCard(t *testing.T) {
	baseURL := newA2ATestServer(t)

	resp, err := http.Get(baseURL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatalf("http.Get agent-card: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}
}

// TestA2AClient_AsTool verifies that a remote A2A agent can be wrapped as a tool.
func TestA2AClient_AsTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	baseURL := newA2ATestServer(t)

	client, err := a2a.NewClient(ctx, baseURL)
	if err != nil {
		t.Fatalf("a2a.NewClient: %v", err)
	}

	tool, err := client.AsTool()
	if err != nil {
		t.Fatalf("client.AsTool: %v", err)
	}

	result, err := tool.Handle(ctx, `{"input":"Greet Alice"}`)
	if err != nil {
		t.Fatalf("tool.Handle: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil tool result")
	}

	t.Logf("A2A tool result: %#v", result)
}

// TestA2A_AgentIntegration verifies that a local agent can delegate to a remote
// A2A agent via the tool bridge.
func TestA2A_AgentIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	baseURL := newA2ATestServer(t)

	remoteClient, err := a2a.NewClient(ctx, baseURL)
	if err != nil {
		t.Fatalf("a2a.NewClient: %v", err)
	}

	greeterTool, err := remoteClient.AsTool()
	if err != nil {
		t.Fatalf("remoteClient.AsTool: %v", err)
	}

	llmClient := buildOpenAILLM()
	orchestrator, err := agent.New(
		llmClient,
		"orchestrator",
		"When asked to greet someone, use the greeter tool instead of greeting directly.",
	)
	if err != nil {
		t.Fatalf("agent.New orchestrator: %v", err)
	}

	if err := orchestrator.AddTool(greeterTool); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, err := orchestrator.Run(ctx, llm.Text("Please greet Alice."))
	if err != nil {
		t.Fatalf("orchestrator.Run: %v", err)
	}

	text := result.TextContent()
	t.Logf("Orchestrator response: %q", text)

	if strings.TrimSpace(text) == "" {
		t.Fatal("expected non-empty orchestrator response")
	}
}
