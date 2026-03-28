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
	"github.com/google/uuid"
	openaiapi "github.com/sashabaranov/go-openai"

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
//
// The boundary types are OpenAI-shaped because `llm.Message` and tool calls are
// aliases of go-openai types.
type Client struct {
	client anthropicapi.Client

	apiKey    string
	model     string
	maxTokens int64
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

	c.client = anthropicapi.NewClient(clientOpts...)
	return c
}

// Execute calls the Anthropic Messages API with the given OpenAI-shaped messages and tools.
//
// It converts the response to an OpenAI-shaped assistant message, including any tool calls.
func (c *Client) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	system, anthropicMessages, err := openAIMessagesToAnthropic(messages)
	if err != nil {
		return nil, err
	}

	params := anthropicapi.MessageNewParams{
		Model:     anthropicapi.Model(strings.TrimSpace(c.model)),
		MaxTokens: c.maxTokens,
		Messages:  anthropicMessages,
		System:    system,
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

	msg, err := anthropicMessageToOpenAI(res)
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

func openAIMessagesToAnthropic(messages []llm.Message) ([]anthropicapi.TextBlockParam, []anthropicapi.MessageParam, error) {
	var systemParts []string
	out := make([]anthropicapi.MessageParam, 0, len(messages))

	for _, m := range messages {
		text := openAIMessageText(m)
		switch m.Role {
		case llm.ChatMessageRoleSystem:
			if strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}

		case llm.ChatMessageRoleUser:
			blocks := make([]anthropicapi.ContentBlockParamUnion, 0, 1)
			if strings.TrimSpace(text) != "" {
				blocks = append(blocks, anthropicapi.NewTextBlock(text))
			}
			out = append(out, anthropicapi.NewUserMessage(blocks...))

		case llm.ChatMessageRoleAssistant:
			blocks := make([]anthropicapi.ContentBlockParamUnion, 0, 1+len(m.ToolCalls))
			if strings.TrimSpace(text) != "" {
				blocks = append(blocks, anthropicapi.NewTextBlock(text))
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

		case llm.ChatMessageRoleTool:
			toolUseID := strings.TrimSpace(m.ToolCallID)
			if toolUseID == "" {
				return nil, nil, ErrToolMessageMissingToolCallID
			}
			// Anthropic expects tool results in a `user` message.
			out = append(out, anthropicapi.NewUserMessage(
				anthropicapi.NewToolResultBlock(toolUseID, m.Content, false),
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

func anthropicMessageToOpenAI(m *anthropicapi.Message) (*llm.Message, error) {
	if m == nil {
		return nil, &NilResponseError{}
	}

	var textParts []string
	toolCalls := make([]llm.ToolCall, 0)

	for _, b := range m.Content {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				textParts = append(textParts, b.Text)
			}
		case "tool_use":
			// b.Input is raw JSON.
			args := strings.TrimSpace(string(b.Input))
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   b.ID,
				Type: llm.ToolTypeFunction,
				Function: openaiapi.FunctionCall{
					Name:      b.Name,
					Arguments: args,
				},
			})
		default:
			// Ignore non-text blocks (thinking, etc) for now.
		}
	}

	content := strings.TrimSpace(strings.Join(textParts, "\n"))
	return &llm.Message{
		Role:      llm.ChatMessageRoleAssistant,
		Content:   content,
		ToolCalls: toolCalls,
	}, nil
}

func openAIMessageText(m llm.Message) string {
	if strings.TrimSpace(m.Content) != "" {
		return m.Content
	}
	if len(m.MultiContent) == 0 {
		return ""
	}

	parts := make([]string, 0, len(m.MultiContent))
	for _, p := range m.MultiContent {
		if strings.TrimSpace(p.Text) != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
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
