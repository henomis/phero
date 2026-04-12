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

package anthropic

import (
	"context"
	"encoding/json"
	"strings"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/google/uuid"

	"github.com/henomis/phero/llm"
)

// Client implements llm.LLM using Anthropic's Messages API.
var _ llm.LLM = (*Client)(nil)

const (
	// DefaultModel is the Anthropic model used when no explicit model option is provided.
	//
	// We keep it as an Anthropic-native name; OpenAI-style model names are accepted
	// too and will be mapped via OpenAIModelToAnthropic.
	DefaultModel = "claude-sonnet-4-6"

	// DefaultMaxTokens is the default max_tokens value used for requests.
	DefaultMaxTokens int64 = 2048
)

// Client is an llm.LLM implementation that uses github.com/anthropics/anthropic-sdk-go.
type Client struct {
	client anthropicapi.Client

	apiKey      string
	baseURL     string
	model       string
	temperature float32
	maxTokens   int64
}

// Option configures a Client created by New.
type Option func(*Client)

// New constructs a new Client with the given API key and applies any options.
//
// If apiKey is empty, the underlying SDK will fall back to environment variables
// (ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN) as per anthropic-sdk-go behavior.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:    strings.TrimSpace(apiKey),
		model:     DefaultModel,
		maxTokens: DefaultMaxTokens,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	clientOpts := []option.RequestOption{}
	if c.apiKey != "" {
		clientOpts = append(clientOpts, option.WithAPIKey(c.apiKey))
	}
	if c.baseURL != "" {
		clientOpts = append(clientOpts, option.WithBaseURL(c.baseURL))
	}

	c.client = anthropicapi.NewClient(clientOpts...)
	return c
}

// Execute calls the Anthropic Messages API with the given messages and tools.
//
// It converts the response to a Phero assistant message, including any tool calls.
func (c *Client) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	system, anthropicMessages, err := messagesToAnthropic(messages)
	if err != nil {
		return nil, err
	}

	params := anthropicapi.MessageNewParams{
		Model:       anthropicapi.Model(strings.TrimSpace(c.model)),
		MaxTokens:   c.maxTokens,
		Messages:    anthropicMessages,
		System:      system,
		Temperature: param.NewOpt(float64(c.temperature)),
	}

	if len(tools) > 0 {
		params.Tools, err = anthropicTools(tools)
		if err != nil {
			return nil, err
		}
	}

	res, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}

	msg, err := anthropicMessageToPhero(res)
	if err != nil {
		return nil, err
	}

	return &llm.Result{
		Message: msg,
		Usage: &llm.Usage{
			InputTokens:  int(res.Usage.InputTokens),
			OutputTokens: int(res.Usage.OutputTokens),
		},
	}, nil
}

// messagesToAnthropic converts Phero messages to Anthropic wire types.
//
// System messages are collected and returned separately as required by the Anthropic API.
func messagesToAnthropic(messages []llm.Message) ([]anthropicapi.TextBlockParam, []anthropicapi.MessageParam, error) {
	var systemParts []string
	out := make([]anthropicapi.MessageParam, 0, len(messages))

	for _, m := range messages {
		switch m.Role {
		case llm.RoleSystem:
			text := m.TextContent()
			if strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}

		case llm.RoleUser:
			blocks, err := contentPartsToAnthropic(m.Parts)
			if err != nil {
				return nil, nil, err
			}
			if len(blocks) > 0 {
				out = append(out, anthropicapi.NewUserMessage(blocks...))
			}

		case llm.RoleAssistant:
			blocks := make([]anthropicapi.ContentBlockParamUnion, 0, len(m.Parts)+len(m.ToolCalls))
			for _, p := range m.Parts {
				if p.Type == llm.ContentTypeText && strings.TrimSpace(p.Text) != "" {
					blocks = append(blocks, anthropicapi.NewTextBlock(p.Text))
				}
			}
			for _, tc := range m.ToolCalls {
				id := strings.TrimSpace(tc.ID)
				if id == "" {
					id = uuid.NewString()
				}

				var input any
				args := strings.TrimSpace(tc.Function.Arguments)
				if args == "" {
					input = map[string]any{}
				} else {
					if err := json.Unmarshal([]byte(args), &input); err != nil {
						return nil, nil, &ToolArgumentsParseError{ToolName: tc.Function.Name, Err: err}
					}
				}
				blocks = append(blocks, anthropicapi.NewToolUseBlock(id, input, tc.Function.Name))
			}
			out = append(out, anthropicapi.NewAssistantMessage(blocks...))

		case llm.RoleTool:
			toolUseID := strings.TrimSpace(m.ToolCallID)
			if toolUseID == "" {
				return nil, nil, ErrToolMessageMissingToolCallID
			}
			// Build tool result content blocks from parts.
			content := buildToolResultContent(m.Parts)
			toolBlock := anthropicapi.ToolResultBlockParam{
				ToolUseID: toolUseID,
				Content:   content,
			}
			out = append(out, anthropicapi.NewUserMessage(
				anthropicapi.ContentBlockParamUnion{OfToolResult: &toolBlock},
			))

		default:
			return nil, nil, &UnsupportedRoleError{Role: m.Role}
		}
	}

	joined := strings.TrimSpace(strings.Join(systemParts, "\n\n"))
	var system []anthropicapi.TextBlockParam
	if joined != "" {
		system = []anthropicapi.TextBlockParam{{Text: joined}}
	}

	return system, out, nil
}

// contentPartsToAnthropic converts Phero ContentParts to Anthropic content block params.
func contentPartsToAnthropic(parts []llm.ContentPart) ([]anthropicapi.ContentBlockParamUnion, error) {
	blocks := make([]anthropicapi.ContentBlockParamUnion, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case llm.ContentTypeText:
			if strings.TrimSpace(p.Text) != "" {
				blocks = append(blocks, anthropicapi.NewTextBlock(p.Text))
			}
		case llm.ContentTypeImageURL:
			blocks = append(blocks, anthropicapi.NewImageBlock(
				anthropicapi.URLImageSourceParam{URL: p.ImageURL},
			))
		case llm.ContentTypeImageBase64:
			blocks = append(blocks, anthropicapi.NewImageBlock(
				anthropicapi.Base64ImageSourceParam{
					Data:      p.ImageBase64,
					MediaType: anthropicapi.Base64ImageSourceMediaType(p.MIMEType),
				},
			))
		}
	}
	return blocks, nil
}

// buildToolResultContent converts Phero ContentParts to Anthropic ToolResultBlockParamContentUnion entries.
//
// Text parts become text blocks; image-URL parts become image blocks.
// This allows tools to return images as part of their result.
func buildToolResultContent(parts []llm.ContentPart) []anthropicapi.ToolResultBlockParamContentUnion {
	content := make([]anthropicapi.ToolResultBlockParamContentUnion, 0, len(parts))
	for _, p := range parts {
		switch p.Type {
		case llm.ContentTypeText:
			text := p.Text
			content = append(content, anthropicapi.ToolResultBlockParamContentUnion{
				OfText: &anthropicapi.TextBlockParam{Text: text},
			})
		case llm.ContentTypeImageURL:
			imgBlock := anthropicapi.ImageBlockParam{
				Source: anthropicapi.ImageBlockParamSourceUnion{
					OfURL: &anthropicapi.URLImageSourceParam{URL: p.ImageURL},
				},
			}
			content = append(content, anthropicapi.ToolResultBlockParamContentUnion{
				OfImage: &imgBlock,
			})
		case llm.ContentTypeImageBase64:
			imgBlock := anthropicapi.ImageBlockParam{
				Source: anthropicapi.ImageBlockParamSourceUnion{
					OfBase64: &anthropicapi.Base64ImageSourceParam{
						Data:      p.ImageBase64,
						MediaType: anthropicapi.Base64ImageSourceMediaType(p.MIMEType),
					},
				},
			}
			content = append(content, anthropicapi.ToolResultBlockParamContentUnion{
				OfImage: &imgBlock,
			})
		}
	}
	return content
}

// anthropicMessageToPhero converts an Anthropic API response to a Phero Message.
func anthropicMessageToPhero(m *anthropicapi.Message) (*llm.Message, error) {
	if m == nil {
		return nil, &NilResponseError{}
	}

	parts := make([]llm.ContentPart, 0)
	toolCalls := make([]llm.ToolCall, 0)

	for _, b := range m.Content {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				parts = append(parts, llm.Text(b.Text))
			}
		case "tool_use":
			args := strings.TrimSpace(string(b.Input))
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   b.ID,
				Type: llm.ToolTypeFunction,
				Function: llm.FunctionCall{
					Name:      b.Name,
					Arguments: args,
				},
			})
		default:
			// Ignore non-text blocks (thinking, etc.) for now.
		}
	}

	msg := &llm.Message{
		Role:      llm.RoleAssistant,
		Parts:     parts,
		ToolCalls: toolCalls,
	}
	return msg, nil
}

func anthropicTools(tools []*llm.Tool) ([]anthropicapi.ToolUnionParam, error) {
	out := make([]anthropicapi.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}
		inputSchema, err := anthropicToolInputSchema(t.InputSchema())
		if err != nil {
			return nil, err
		}
		tool := anthropicapi.ToolParam{
			Name:        t.Name(),
			Description: anthropicapi.String(t.Description()),
			Strict:      anthropicapi.Bool(true),
			InputSchema: inputSchema,
		}
		out = append(out, anthropicapi.ToolUnionParam{OfTool: &tool})
	}
	return out, nil
}

func anthropicToolInputSchema(schema map[string]any) (anthropicapi.ToolInputSchemaParam, error) {
	if schema == nil {
		return anthropicapi.ToolInputSchemaParam{Properties: map[string]any{}}, nil
	}

	properties := schema["properties"]

	required := make([]string, 0)
	if raw, ok := schema["required"]; ok {
		switch v := raw.(type) {
		case []string:
			required = append(required, v...)
		case []any:
			for _, it := range v {
				s, ok := it.(string)
				if ok {
					required = append(required, s)
				}
			}
		}
	}

	extra := make(map[string]any)
	for k, v := range schema {
		if k == "type" || k == "properties" || k == "required" {
			continue
		}
		extra[k] = v
	}

	return anthropicapi.ToolInputSchemaParam{
		Properties:  properties,
		Required:    required,
		ExtraFields: extra,
	}, nil
}

// WithBaseURL overrides the Anthropic API base URL.
//
// This is useful for routing requests through a proxy or using a compatible
// endpoint. In tests it can point to an httptest.Server.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimSpace(baseURL)
	}
}

// WithModel sets the Anthropic model name used for requests (e.g. "claude-...").
func WithModel(model string) Option {
	return func(c *Client) {
		c.model = strings.TrimSpace(model)
	}
}

// WithMaxTokens sets the max_tokens parameter used for Anthropic requests.
func WithMaxTokens(maxTokens int64) Option {
	return func(c *Client) {
		if maxTokens > 0 {
			c.maxTokens = maxTokens
		}
	}
}

func WithTemperature(temp float32) Option {
	return func(c *Client) {
		c.temperature = temp
	}
}
