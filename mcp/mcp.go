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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henomis/phero/llm"
)

// ToolFilter decides whether a tool (by name) should be exposed.
//
// Returning false excludes the tool.
type ToolFilter func(toolName string) bool

// Server wraps an MCP client session and can expose remote MCP tools as
// `llm.FunctionTool` values.
type Server struct {
	session *mcp.ClientSession
}

// New creates a new Server bound to the provided MCP client session.
func New(session *mcp.ClientSession) *Server {
	return &Server{
		session: session,
	}
}

// AsTools lists tools from the underlying MCP session and converts them into
// `llm.FunctionTool` values.
//
// The optional filter can be used to include/exclude tools by name.
func (s *Server) AsTools(ctx context.Context, filter ToolFilter) ([]*llm.Tool, error) {
	tools, err := s.session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}

	functionTools := make([]*llm.Tool, 0)

	for _, tool := range tools.Tools {
		if filter != nil && !filter(tool.Name) {
			continue
		}

		if _, ok := tool.InputSchema.(map[string]any); !ok {
			return nil, fmt.Errorf("invalid tool input schema for tool %q: expected an object", tool.Name)
		}

		// Ensure the input schema has a "properties" field, even if empty, to be compatible with OpenAI function calling.
		if _, ok := tool.InputSchema.(map[string]any)["properties"]; !ok {
			tool.InputSchema.(map[string]any)["properties"] = map[string]any{}
		}

		newTool, err := llm.NewTool(
			tool.Name,
			tool.Description,
			func(ctx context.Context, arguments string) (any, error) {
				argumentsAsMap := make(map[string]any)
				if err := json.Unmarshal([]byte(arguments), &argumentsAsMap); err != nil {
					return nil, fmt.Errorf("invalid tool arguments: %w", err)
				}

				result, err := s.session.CallTool(
					ctx,
					&mcp.CallToolParams{
						Name:      tool.Name,
						Arguments: argumentsAsMap,
					},
				)
				if err != nil {
					return nil, err
				}

				return callToolResultText(result), nil
			},
		)
		if err != nil {
			return nil, err
		}

		functionTools = append(functionTools, newTool)
	}

	return functionTools, nil
}

// callToolResultText extracts a readable textual representation of an MCP tool
// call result.
func callToolResultText(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	parts := make([]string, 0, len(res.Content))
	for _, c := range res.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			if strings.TrimSpace(v.Text) != "" {
				parts = append(parts, strings.TrimSpace(v.Text))
			}
		default:
			parts = append(parts, fmt.Sprintf("[%T]", c))
		}
	}
	return strings.Join(parts, "\n")
}
