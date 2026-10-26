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
	"maps"
	"slices"
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

	// Names providers refuse: a dot, a clash once sanitized, too long.
	for _, name := range []string{"files.read", "files_read", strings.Repeat("x", 70)} {
		srv.AddTool(&mcp.Tool{Name: name, InputSchema: schema},
			func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: req.Params.Name}}}, nil
			})
	}

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
	for _, tt := range []struct {
		max  int
		want string
	}{
		// The 30-byte note fits in 51: 21 bytes are left for the text, ten
		// two-byte characters, as the eleventh is not split.
		{max: 51, want: strings.Repeat("é", 10) + "\n[output cut at 51 characters]"},
		// 11 bytes cannot hold the note: the text alone, still not split.
		{max: 11, want: strings.Repeat("é", 5)},
	} {
		long := toolsByName(t, connectTestServer(t, WithMaxResultChars(tt.max)))["long"]

		out, err := long.Handle(context.Background(), "{}")
		if err != nil {
			t.Fatal(err)
		}

		if s, _ := out.(string); s != tt.want || len(s) > tt.max {
			t.Fatalf("max %d: result = %q (%d bytes), want %q", tt.max, s, len(s), tt.want)
		}
	}

	whole, err := toolsByName(t, connectTestServer(t))["long"].Handle(context.Background(), "{}")
	if err != nil || whole != strings.Repeat("é", 100) {
		t.Fatalf("without a cap: result = %q, %v; want the whole text", whole, err)
	}
}

func TestAsTools_NamesProvidersAccept(t *testing.T) {
	tools := toolsByName(t, connectTestServer(t))

	long := strings.Repeat("x", 64)
	for name, mcpName := range map[string]string{
		"files_read": "files.read", "files_read_2": "files_read", long: strings.Repeat("x", 70),
	} {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("no tool %q in %v", name, slices.Collect(maps.Keys(tools)))
		}

		// The MCP server is still called with the tool's own name.
		out, err := tool.Handle(context.Background(), "{}")
		if err != nil || out != mcpName {
			t.Fatalf("%s: result = %v, %v; want a call to %q", name, out, err, mcpName)
		}
	}
}

func TestUniqueToolName(t *testing.T) {
	taken := map[string]bool{}
	long := strings.Repeat("y", 64)

	for _, want := range []string{long, long[:62] + "_2", long[:62] + "_3"} {
		if got := uniqueToolName(long, taken); got != want {
			t.Fatalf("uniqueToolName = %q, want %q", got, want)
		}
	}
}
