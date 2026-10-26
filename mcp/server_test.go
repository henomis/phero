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

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henomis/phero/v2/llm"
)

// connectTestServer starts an in-memory MCP server with a failing tool, an
// image tool and a tool with a long text result, and returns a Server wrapping a client session to it.
func connectTestServer(t *testing.T, opts ...Option) *Server {
	t.Helper()

	ctx := context.Background()
	schema := map[string]any{"type": "object"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	srv.AddTool(&mcp.Tool{Name: "fail", InputSchema: schema},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "disk full"}}}, nil
		})
	srv.AddTool(&mcp.Tool{Name: "shot", InputSchema: schema},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: "screenshot"},
				&mcp.ImageContent{MIMEType: "image/png", Data: []byte("png")},
			}}, nil
		})

	srv.AddTool(&mcp.Tool{Name: "long", InputSchema: schema},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("é", 100)}}}, nil
		})

	serverT, clientT := mcp.NewInMemoryTransports()

	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ss.Close() })

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client", Version: "v0"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = cs.Close() })

	return New(cs, opts...)
}

func toolsByName(t *testing.T, s *Server) map[string]*llm.Tool {
	t.Helper()

	tools, err := s.AsTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	byName := make(map[string]*llm.Tool, len(tools))
	for _, tl := range tools {
		byName[tl.Name()] = tl
	}

	return byName
}

func TestAsTools_IsErrorReturnsError(t *testing.T) {
	_, err := toolsByName(t, connectTestServer(t))["fail"].Handle(context.Background(), "{}")

	var resErr *ToolResultError
	if !errors.As(err, &resErr) || resErr.Name != "fail" || resErr.Message != "disk full" {
		t.Fatalf("err = %v, want ToolResultError with the result text", err)
	}
}

func TestAsTools_ImageContentBecomesImagePart(t *testing.T) {
	out, err := toolsByName(t, connectTestServer(t))["shot"].Handle(context.Background(), "{}")
	if err != nil {
		t.Fatal(err)
	}

	parts, ok := out.([]llm.ContentPart)
	if !ok || len(parts) != 2 {
		t.Fatalf("result = %#v, want text and image parts", out)
	}

	if parts[0].Text != "screenshot" || parts[1].Type != llm.ContentTypeImageBase64 ||
		parts[1].MIMEType != "image/png" || parts[1].ImageBase64 != "cG5n" {
		t.Fatalf("parts = %+v", parts)
	}
}

func TestAsTools_ToolPrefix(t *testing.T) {
	tools := toolsByName(t, connectTestServer(t, WithToolPrefix("srv_")))

	shot, ok := tools["srv_shot"]
	if !ok {
		t.Fatalf("tools = %v, want srv_shot", tools)
	}

	// The MCP server is still called with the tool's own name.
	if _, err := shot.Handle(context.Background(), "{}"); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}

func TestAsTools_MaxResultChars(t *testing.T) {
	long := toolsByName(t, connectTestServer(t, WithMaxResultChars(11)))["long"]

	out, err := long.Handle(context.Background(), "{}")
	if err != nil {
		t.Fatal(err)
	}

	// 11 bytes hold five two-byte characters; the sixth is not split.
	if want := strings.Repeat("é", 5) + "\n[output cut at 11 characters]"; out != want {
		t.Fatalf("result = %q, want %q", out, want)
	}

	whole, err := toolsByName(t, connectTestServer(t))["long"].Handle(context.Background(), "{}")
	if err != nil || whole != strings.Repeat("é", 100) {
		t.Fatalf("without a cap: result = %q, %v; want the whole text", whole, err)
	}
}
