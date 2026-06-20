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

	// DefaultTemperature is the default sampling temperature used for requests.
	//
	// It matches the Anthropic Messages API default so that an unconfigured client
	// behaves identically to omitting the parameter.
	DefaultTemperature float32 = 1.0
)

// Client is an llm.LLM implementation that uses github.com/anthropics/anthropic-sdk-go.
type Client struct {
	client anthropicapi.Client

	apiKey         string
	baseURL        string
	model          string
	temperature    float32
	maxTokens      int64
	promptCaching  bool
	thinkingbudget int64
}

// Option configures a Client created by New.
type Option func(*Client)

// New constructs a new Client with the given API key and applies any options.
//
// If apiKey is empty, the underlying SDK will fall back to environment variables
// (ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN) as per anthropic-sdk-go behavior.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:      strings.TrimSpace(apiKey),
		model:       DefaultModel,
		maxTokens:   DefaultMaxTokens,
		temperature: DefaultTemperature,
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
	params, err := c.buildParams(messages, tools)
	if err != nil {
		return nil, err
	}

	res, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, err
	}

	msg, err := messageFromAnthropic(res)
	if err != nil {
		return nil, err
	}

	model := string(res.Model)
	if model == "" {
		model = strings.TrimSpace(c.model)
	}

	return &llm.Result{
		Message: msg,
		Model:   model,
		Usage: &llm.Usage{
			InputTokens:      int(res.Usage.InputTokens),
			OutputTokens:     int(res.Usage.OutputTokens),
			CacheReadTokens:  int(res.Usage.CacheReadInputTokens),
			CacheWriteTokens: int(res.Usage.CacheCreationInputTokens),
		},
	}, nil
}

// buildParams converts Phero messages and tools into Anthropic request params,
// applying the thinking, temperature, max-tokens, and prompt-caching options.
// It is shared by the buffered Execute and the streaming ExecuteStream.
func (c *Client) buildParams(messages []llm.Message, tools []*llm.Tool) (anthropicapi.MessageNewParams, error) {
	system, anthropicMessages, err := messagesToAnthropic(messages)
	if err != nil {
		return anthropicapi.MessageNewParams{}, err
	}

	maxTokens := c.maxTokens
	params := anthropicapi.MessageNewParams{
		Model:    anthropicapi.Model(strings.TrimSpace(c.model)),
		Messages: anthropicMessages,
		System:   system,
	}

	if c.thinkingbudget > 0 {
		// Extended thinking requires max_tokens > budget and disallows a custom
		// temperature, so we omit temperature and ensure headroom above the budget.
		if maxTokens <= c.thinkingbudget {
			maxTokens = c.thinkingbudget + c.maxTokens
		}

		params.Thinking = anthropicapi.ThinkingConfigParamOfEnabled(c.thinkingbudget)
	} else {
		params.Temperature = param.NewOpt(float64(c.temperature))
	}

	params.MaxTokens = maxTokens

	if len(tools) > 0 {
		params.Tools = anthropicTools(tools)
	}

	if c.promptCaching {
		applyPromptCaching(&params)
	}

	return params, nil
}

// applyPromptCaching marks the high-value, stable prefix of the request as
// cacheable: the (last) system block and the last tool definition. Anthropic
// caches everything up to and including a cache_control breakpoint, so marking
// these two points covers the system prompt and the full tool list.
func applyPromptCaching(params *anthropicapi.MessageNewParams) {
	if n := len(params.System); n > 0 {
		params.System[n-1].CacheControl = anthropicapi.NewCacheControlEphemeralParam()
	}

	if n := len(params.Tools); n > 0 {
		if tool := params.Tools[n-1].OfTool; tool != nil {
			tool.CacheControl = anthropicapi.NewCacheControlEphemeralParam()
		}
	}
}

// messagesToAnthropic converts Phero messages to Anthropic wire types.
//
// System messages are collected and returned separately as required by the Anthropic API.
//
// Consecutive messages that map to the same Anthropic role are merged into a
// single turn. This is required for correctness: Phero (like OpenAI) models each
// tool result as its own RoleTool message, but Anthropic requires that all
// tool_result blocks answering a parallel-tool-call turn be delivered together in
// a single user message — and that no other message sit between the assistant's
// tool_use turn and that user turn. Without merging, parallel tool calls produce
// separate consecutive user messages, which Anthropic rejects (or which silently
// degrades future parallel tool use).
func messagesToAnthropic(messages []llm.Message) ([]anthropicapi.TextBlockParam, []anthropicapi.MessageParam, error) {
	var systemParts []string

	out := make([]anthropicapi.MessageParam, 0, len(messages))

	// pending accumulates content blocks for consecutive messages that share the
	// same Anthropic role (user or assistant); flush emits the merged turn.
	var (
		pendingRole string
		pending     []anthropicapi.ContentBlockParamUnion
	)

	flush := func() {
		if len(pending) == 0 {
			return
		}

		switch pendingRole {
		case llm.RoleAssistant:
			out = append(out, anthropicapi.NewAssistantMessage(pending...))
		default:
			out = append(out, anthropicapi.NewUserMessage(pending...))
		}

		pending = nil
		pendingRole = ""
	}

	// add appends blocks under the given Anthropic role, flushing first if the
	// role changes so each emitted turn holds a single role's blocks.
	add := func(role string, blocks ...anthropicapi.ContentBlockParamUnion) {
		if len(blocks) == 0 {
			return
		}

		if pendingRole != "" && pendingRole != role {
			flush()
		}

		pendingRole = role

		pending = append(pending, blocks...)
	}

	for _, m := range messages {
		switch m.Role {
		case llm.RoleSystem:
			text := m.TextContent()
			if strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}

		case llm.RoleUser:
			blocks := contentBlocksToAnthropic(m.Parts)

			add(llm.RoleUser, blocks...)

		case llm.RoleAssistant:
			blocks := make([]anthropicapi.ContentBlockParamUnion, 0, len(m.Parts)+len(m.ToolCalls))
			// Thinking blocks must precede other content. Anthropic also requires
			// them (with their signature) to be replayed when the turn is followed
			// by tool results under extended thinking.
			for _, p := range m.Parts {
				switch p.Type {
				case llm.ContentTypeReasoning:
					if strings.TrimSpace(p.Text) != "" {
						blocks = append(blocks, anthropicapi.NewThinkingBlock(p.Signature, p.Text))
					}
				case llm.ContentTypeRedactedReasoning:
					if p.Text != "" {
						blocks = append(blocks, anthropicapi.NewRedactedThinkingBlock(p.Text))
					}
				}
			}

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

			add(llm.RoleAssistant, blocks...)

		case llm.RoleTool:
			toolUseID := strings.TrimSpace(m.ToolCallID)
			if toolUseID == "" {
				return nil, nil, ErrToolMessageMissingToolCallID
			}
			// Build tool result content blocks from parts. Tool results map to the
			// user role and are merged with adjacent tool results into one turn.
			content := toolResultContentToAnthropic(m.Parts)

			toolBlock := anthropicapi.ToolResultBlockParam{
				ToolUseID: toolUseID,
				Content:   content,
			}
			if m.ToolError {
				toolBlock.IsError = param.NewOpt(true)
			}

			add(llm.RoleUser, anthropicapi.ContentBlockParamUnion{OfToolResult: &toolBlock})

		default:
			return nil, nil, &UnsupportedRoleError{Role: m.Role}
		}
	}

	flush()

	joined := strings.TrimSpace(strings.Join(systemParts, "\n\n"))

	var system []anthropicapi.TextBlockParam
	if joined != "" {
		system = []anthropicapi.TextBlockParam{{Text: joined}}
	}

	return system, out, nil
}

// contentBlocksToAnthropic converts Phero ContentParts to Anthropic content block params.
func contentBlocksToAnthropic(parts []llm.ContentPart) []anthropicapi.ContentBlockParamUnion {
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

	return blocks
}

// toolResultContentToAnthropic converts Phero ContentParts to Anthropic ToolResultBlockParamContentUnion entries.
//
// Text parts become text blocks; image-URL parts become image blocks.
// This allows tools to return images as part of their result.
func toolResultContentToAnthropic(parts []llm.ContentPart) []anthropicapi.ToolResultBlockParamContentUnion {
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

// messageFromAnthropic converts an Anthropic API response to a Phero Message.
func messageFromAnthropic(m *anthropicapi.Message) (*llm.Message, error) {
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
		case "thinking":
			if strings.TrimSpace(b.Thinking) != "" {
				parts = append(parts, llm.Reasoning(b.Thinking, b.Signature))
			}
		case "redacted_thinking":
			if b.Data != "" {
				parts = append(parts, llm.RedactedReasoning(b.Data))
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
			// Ignore other block kinds (server tool results, etc.).
		}
	}

	msg := &llm.Message{
		Role:      llm.RoleAssistant,
		Parts:     parts,
		ToolCalls: toolCalls,
	}

	return msg, nil
}

func anthropicTools(tools []*llm.Tool) []anthropicapi.ToolUnionParam {
	out := make([]anthropicapi.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t == nil {
			continue
		}

		inputSchema := anthropicToolInputSchema(t.InputSchema())

		tool := anthropicapi.ToolParam{
			Name:        t.Name(),
			Description: anthropicapi.String(t.Description()),
			Strict:      anthropicapi.Bool(true),
			InputSchema: inputSchema,
		}
		out = append(out, anthropicapi.ToolUnionParam{OfTool: &tool})
	}

	return out
}

func anthropicToolInputSchema(schema map[string]any) anthropicapi.ToolInputSchemaParam {
	if schema == nil {
		return anthropicapi.ToolInputSchemaParam{Properties: map[string]any{}}
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
	}
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

// WithTemperature sets the sampling temperature used for Anthropic requests.
//
// When unset, the client uses DefaultTemperature, which matches the Anthropic
// Messages API default. Temperature is ignored when extended thinking is enabled
// via WithThinking (Anthropic requires the default temperature in that mode).
func WithTemperature(temp float32) Option {
	return func(c *Client) {
		c.temperature = temp
	}
}

// WithPromptCaching enables Anthropic prompt caching.
//
// When enabled, the client marks the system prompt and the tool list as cacheable
// (an ephemeral cache_control breakpoint), so repeated requests that share that
// stable prefix are billed at the much cheaper cache-read rate. Cache read/write
// token counts are reported on Result.Usage (CacheReadTokens / CacheWriteTokens).
func WithPromptCaching() Option {
	return func(c *Client) {
		c.promptCaching = true
	}
}

// WithThinking enables extended thinking with the given token budget.
//
// budgetTokens is the maximum number of tokens the model may spend reasoning; it
// must be positive to take effect. Reasoning is returned as ContentTypeReasoning
// parts on the assistant message (use Message.ReasoningContent to read it) and is
// replayed on later turns so tool use works under extended thinking. When enabled,
// max_tokens is raised above the budget if needed and the temperature option is
// not sent, as required by the Anthropic API.
func WithThinking(budgetTokens int64) Option {
	return func(c *Client) {
		if budgetTokens > 0 {
			c.thinkingbudget = budgetTokens
		}
	}
}
