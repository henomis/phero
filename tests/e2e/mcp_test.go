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
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	phmcp "github.com/henomis/phero/mcp"
)

func newMCPSession(t *testing.T, ctx context.Context) *gomcp.ClientSession {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	client := gomcp.NewClient(&gomcp.Implementation{Name: "e2e-client", Version: "1.0.0"}, nil)
	serverPath := filepath.Join(filepath.Dir(thisFile), "testdata", "mcp-echo-server")
	cmd := exec.CommandContext(ctx, "go", "run", serverPath)
	transport := &gomcp.CommandTransport{Command: cmd}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}

	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Logf("session.Close: %v", err)
		}
	})

	return session
}

// TestMCP_AsTools verifies that remote MCP tools are exposed as llm tools.
func TestMCP_AsTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session := newMCPSession(t, ctx)
	server := phmcp.New(session)

	tools, err := server.AsTools(ctx, nil)
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}

	if len(tools) == 0 {
		t.Fatal("expected at least one MCP tool")
	}

	if tools[0].Name() == "" {
		t.Fatal("expected tool name")
	}

	t.Logf("MCP tool: %s", tools[0].Name())
}

// TestMCP_Filter verifies that the filter excludes tools by name.
func TestMCP_Filter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session := newMCPSession(t, ctx)
	server := phmcp.New(session)

	tools, err := server.AsTools(ctx, func(name string) bool { return name == "does-not-exist" })
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}

	if len(tools) != 0 {
		t.Fatalf("expected 0 tools after filtering, got %d", len(tools))
	}
}

// TestMCP_ToolInvocation verifies that the bridged llm tool can call the remote MCP tool.
func TestMCP_ToolInvocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	session := newMCPSession(t, ctx)
	server := phmcp.New(session)

	tools, err := server.AsTools(ctx, nil)
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	result, err := tools[0].Handle(ctx, `{"message":"hello"}`)
	if err != nil {
		t.Fatalf("tool.Handle: %v", err)
	}

	text, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	if text == "" {
		t.Fatal("expected non-empty tool result")
	}

	t.Logf("MCP tool result: %q", text)
}

// TestMCP_AgentIntegration verifies that MCP tools can be used by an agent.
func TestMCP_AgentIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	session := newMCPSession(t, ctx)
	server := phmcp.New(session)

	tools, err := server.AsTools(ctx, nil)
	if err != nil {
		t.Fatalf("AsTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	llmClient := buildOpenAILLM()
	a, err := agent.New(llmClient, "mcp-agent", "Use the available tool when asked to echo a message.")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	for _, tool := range tools {
		if err := a.AddTool(tool); err != nil {
			t.Fatalf("AddTool(%s): %v", tool.Name(), err)
		}
	}

	result, err := a.Run(ctx, llm.Text("Use the echo tool to echo the word hello."))
	if err != nil {
		t.Fatalf("agent.Run: %v", err)
	}

	t.Logf("Agent response: %q", result.TextContent())
}
