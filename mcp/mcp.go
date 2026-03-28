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
	"maps"
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

		schema, err := normalizeMCPInputSchema(tool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("invalid tool input schema for tool %q: %w", tool.Name, err)
		}

		toolName := tool.Name
		newTool, err := llm.NewRawTool(
			toolName,
			tool.Description,
			schema,
			func(ctx context.Context, arguments string) (any, error) {
				argumentsAsMap := make(map[string]any)
				if err := json.Unmarshal([]byte(arguments), &argumentsAsMap); err != nil {
					return nil, fmt.Errorf("invalid tool arguments: %w", err)
				}

				result, err := s.session.CallTool(
					ctx,
					&mcp.CallToolParams{
						Name:      toolName,
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

// normalizeMCPInputSchema validates and clones the raw InputSchema value from an MCP
// tool into a normalized map[string]any ready for the LLM layer.
//
// It never mutates the caller-supplied value. A shallow clone of the top-level map
// is sufficient here because llm.NewRawTool performs a full deep clone via JSON
// round-trip before storing the schema.
func normalizeMCPInputSchema(raw any) (map[string]any, error) {
	schemaMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected an object schema, got %T", raw)
	}

	// Shallow-clone the top level so we never add keys to the SDK-owned map.
	clone := make(map[string]any, len(schemaMap)+1)
	maps.Copy(clone, schemaMap)

	// Ensure a "properties" key exists for OpenAI function-calling compatibility.
	if _, ok := clone["properties"]; !ok {
		clone["properties"] = map[string]any{}
	}

	return clone, nil
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
