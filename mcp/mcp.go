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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henomis/phero/v2/llm"
)

// ToolFilter decides whether a tool (by name) should be exposed.
//
// Returning false excludes the tool.
type ToolFilter func(toolName string) bool

// Server wraps an MCP client session and can expose remote MCP tools as
// `llm.FunctionTool` values.
type Server struct {
	session    *mcp.ClientSession
	toolPrefix string
}

// Option configures a Server.
type Option func(*Server)

// WithToolPrefix prepends prefix to the name of every tool the Server
// exposes, so tools of different servers with the same name do not clash.
// The MCP server is still called with the tool's own name.
func WithToolPrefix(prefix string) Option {
	return func(s *Server) { s.toolPrefix = prefix }
}

// New creates a new Server bound to the provided MCP client session.
func New(session *mcp.ClientSession, opts ...Option) *Server {
	s := &Server{
		session: session,
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
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

		schema, schemaErr := normalizeMCPInputSchema(tool.InputSchema)
		if schemaErr != nil {
			return nil, fmt.Errorf("invalid tool input schema for tool %q: %w", tool.Name, schemaErr)
		}

		toolName := tool.Name

		newTool, newToolErr := llm.NewRawTool(
			s.toolPrefix+toolName,
			tool.Description,
			schema,
			func(ctx context.Context, arguments string) (any, error) {
				argumentsAsMap := make(map[string]any)
				if unmarshalErr := json.Unmarshal([]byte(arguments), &argumentsAsMap); unmarshalErr != nil {
					return nil, fmt.Errorf("invalid tool arguments: %w", unmarshalErr)
				}

				callResult, callErr := s.session.CallTool(
					ctx,
					&mcp.CallToolParams{
						Name:      toolName,
						Arguments: argumentsAsMap,
					},
				)
				if callErr != nil {
					return nil, callErr
				}

				if callResult != nil && callResult.IsError {
					return nil, &ToolResultError{Name: toolName, Message: callToolResultText(callResult)}
				}

				return callToolResultParts(callResult), nil
			},
		)
		if newToolErr != nil {
			return nil, newToolErr
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
	if _, hasProperties := clone["properties"]; !hasProperties {
		clone["properties"] = map[string]any{}
	}

	return clone, nil
}

// callToolResultParts converts an MCP tool call result into content parts: its
// text as one text part, followed by its images. A result with no image keeps
// the plain text form of callToolResultText.
func callToolResultParts(res *mcp.CallToolResult) any {
	text := callToolResultText(res)

	var images []llm.ContentPart

	if res != nil {
		for _, c := range res.Content {
			if img, ok := c.(*mcp.ImageContent); ok && len(img.Data) > 0 {
				images = append(images, llm.ImageBase64(img.MIMEType, base64.StdEncoding.EncodeToString(img.Data)))
			}
		}
	}

	if len(images) == 0 {
		return text
	}

	parts := make([]llm.ContentPart, 0, len(images)+1)
	if text != "" {
		parts = append(parts, llm.Text(text))
	}

	return append(parts, images...)
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
		case *mcp.ImageContent:
			// Returned as an image part by callToolResultParts.
		default:
			parts = append(parts, fmt.Sprintf("[%T]", c))
		}
	}

	return strings.Join(parts, "\n")
}
