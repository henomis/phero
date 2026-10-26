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

package google

import (
	"context"
	"math"
	"strings"

	"google.golang.org/genai"

	"github.com/henomis/phero/v2/llm"
)

// Client implements llm.LLM using the Gemini API.
var _ llm.LLM = (*Client)(nil)

// DefaultModel is the Gemini model used when no explicit model option is
// provided.
const DefaultModel = "gemini-3.5-flash"

// ThinkingLevel is how much a Gemini 3 model thinks before answering. Leaving
// it unset keeps the model's default.
type ThinkingLevel string

const (
	// ThinkingMinimal thinks as little as the model allows.
	ThinkingMinimal ThinkingLevel = ThinkingLevel(genai.ThinkingLevelMinimal)
	// ThinkingLow favors latency and cost.
	ThinkingLow ThinkingLevel = ThinkingLevel(genai.ThinkingLevelLow)
	// ThinkingMedium balances depth and cost.
	ThinkingMedium ThinkingLevel = ThinkingLevel(genai.ThinkingLevelMedium)
	// ThinkingHigh thinks the most.
	ThinkingHigh ThinkingLevel = ThinkingLevel(genai.ThinkingLevelHigh)
)

// Client is an llm.LLM implementation that uses google.golang.org/genai.
type Client struct {
	client  *genai.Client
	initErr error

	apiKey          string
	baseURL         string
	model           string
	temperature     float32
	temperatureSet  bool
	maxTokens       int32
	thinkingLevel   ThinkingLevel
	thinkingBudget  *int32
	includeThoughts bool
}

// Option configures a Client created by New.
type Option func(*Client)

// New constructs a new Client with the given API key and applies any options.
//
// If apiKey is empty, the SDK falls back to the GOOGLE_API_KEY or
// GEMINI_API_KEY environment variable. Creating the SDK client does no I/O; if
// it fails (no key anywhere), every call returns a *ClientInitError.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey: strings.TrimSpace(apiKey),
		model:  DefaultModel,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	cfg := &genai.ClientConfig{APIKey: c.apiKey, Backend: genai.BackendGeminiAPI}
	if c.baseURL != "" {
		cfg.HTTPOptions.BaseURL = c.baseURL
	}

	client, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		c.initErr = &ClientInitError{Err: err}
	}

	c.client = client

	return c
}

// Execute calls the Gemini API with the given messages and call options and
// converts the answer to a phero assistant message, including any tool calls.
func (c *Client) Execute(ctx context.Context, messages []llm.Message, opts ...llm.CallOption) (*llm.Result, error) {
	contents, config, err := c.buildRequest(messages, opts)
	if err != nil {
		return nil, err
	}

	res, err := c.client.Models.GenerateContent(ctx, c.model, contents, config)
	if err != nil {
		return nil, wrapAPIError(err)
	}

	parts, err := candidateParts(res)
	if err != nil {
		return nil, err
	}

	msg := messageFromParts(parts)
	raw := string(res.Candidates[0].FinishReason)

	return &llm.Result{
		Message:         msg,
		Model:           c.modelName(res.ModelVersion),
		Usage:           usageFromGenai(res.UsageMetadata),
		FinishReason:    finishReasonFromGenai(raw, msg),
		RawFinishReason: raw,
	}, nil
}

// Provider returns "google".
func (c *Client) Provider() string { return providerName }

// Model returns the configured model.
func (c *Client) Model() string { return c.model }

// genaiFinishReasons maps Gemini finish reasons to the common set. Reasons
// missing from the map map to llm.FinishError.
var genaiFinishReasons = map[genai.FinishReason]llm.FinishReason{ //nolint:gochecknoglobals // read-only table
	"":                                       "",
	genai.FinishReasonUnspecified:            "",
	genai.FinishReasonStop:                   llm.FinishStop,
	genai.FinishReasonMaxTokens:              llm.FinishLength,
	genai.FinishReasonSafety:                 llm.FinishContentFilter,
	genai.FinishReasonRecitation:             llm.FinishContentFilter,
	genai.FinishReasonBlocklist:              llm.FinishContentFilter,
	genai.FinishReasonProhibitedContent:      llm.FinishContentFilter,
	genai.FinishReasonSPII:                   llm.FinishContentFilter,
	genai.FinishReasonImageSafety:            llm.FinishContentFilter,
	genai.FinishReasonImageProhibitedContent: llm.FinishContentFilter,
	genai.FinishReasonImageRecitation:        llm.FinishContentFilter,
}

// finishReasonFromGenai maps a Gemini finish reason to the common set. Gemini
// reports STOP when it calls tools, so a message with tool calls maps to
// llm.FinishToolCalls.
func finishReasonFromGenai(raw string, msg *llm.Message) llm.FinishReason {
	r, ok := genaiFinishReasons[genai.FinishReason(raw)]
	if !ok {
		return llm.FinishError
	}

	if r == llm.FinishStop && msg != nil && len(msg.ToolCalls) > 0 {
		return llm.FinishToolCalls
	}

	return r
}

// buildRequest converts phero messages and call options into Gemini contents
// and a generation config. It is shared by Execute and ExecuteStream.
func (c *Client) buildRequest(
	messages []llm.Message, opts []llm.CallOption,
) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	if c.initErr != nil {
		return nil, nil, c.initErr
	}

	cfg := llm.NewCallConfig(opts...)
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}

	system, contents, err := contentsFromMessages(messages)
	if err != nil {
		return nil, nil, err
	}

	config := &genai.GenerateContentConfig{SystemInstruction: system}

	if c.temperatureSet {
		t := c.temperature
		config.Temperature = &t
	}

	if c.maxTokens > 0 {
		config.MaxOutputTokens = c.maxTokens
	}

	if cfg.MaxOutputTokens > 0 {
		config.MaxOutputTokens = int32(min(cfg.MaxOutputTokens, math.MaxInt32)) //nolint:gosec // bounded above
	}

	if c.thinkingLevel != "" || c.thinkingBudget != nil || c.includeThoughts {
		config.ThinkingConfig = &genai.ThinkingConfig{
			ThinkingLevel:   genai.ThinkingLevel(c.thinkingLevel),
			ThinkingBudget:  c.thinkingBudget,
			IncludeThoughts: c.includeThoughts,
		}
	}

	if f := cfg.ResponseFormat; f != nil {
		config.ResponseMIMEType = "application/json"
		config.ResponseJsonSchema = outputSchema(f)
	}

	// The tool config goes only with tools: Validate already rejects required
	// and forced choices without them, and with no tools auto and none hold
	// anyway.
	if len(cfg.Tools) > 0 {
		config.Tools = toolsToGenai(cfg.Tools)

		if cfg.ToolChoice != nil {
			config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: toolChoiceToGenai(cfg.ToolChoice)}
		}
	}

	return contents, config, nil
}

// modelName is the model that answered, falling back to the requested one.
func (c *Client) modelName(version string) string {
	if version != "" {
		return version
	}

	return c.model
}

// candidateParts returns the parts of the first candidate, or a
// *NoContentError when there are none (a blocked prompt or answer).
func candidateParts(res *genai.GenerateContentResponse) ([]*genai.Part, error) {
	if res == nil {
		return nil, &NoContentError{}
	}

	if len(res.Candidates) == 0 || res.Candidates[0] == nil {
		e := &NoContentError{}
		if res.PromptFeedback != nil {
			e.BlockReason = string(res.PromptFeedback.BlockReason)
		}

		return nil, e
	}

	cand := res.Candidates[0]
	if cand.Content == nil || len(cand.Content.Parts) == 0 {
		return nil, &NoContentError{FinishReason: string(cand.FinishReason)}
	}

	return cand.Content.Parts, nil
}

// usageFromGenai maps Gemini's token counts to phero's: input excludes the
// cached tokens, which are reported apart, and output includes the thinking
// tokens, which are billed as output.
func usageFromGenai(u *genai.GenerateContentResponseUsageMetadata) *llm.Usage {
	if u == nil {
		return nil
	}

	cached := int(u.CachedContentTokenCount)

	return &llm.Usage{
		InputTokens:     max(int(u.PromptTokenCount)-cached, 0),
		OutputTokens:    int(u.CandidatesTokenCount) + int(u.ThoughtsTokenCount),
		CacheReadTokens: cached,
		ReasoningTokens: int(u.ThoughtsTokenCount),
	}
}

// WithBaseURL overrides the Gemini API base URL, for a proxy or, in tests, an
// httptest.Server.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimSpace(baseURL)
	}
}

// WithModel sets the Gemini model name used for requests (e.g. "gemini-3.5-flash").
func WithModel(model string) Option {
	return func(c *Client) {
		if m := strings.TrimSpace(model); m != "" {
			c.model = m
		}
	}
}

// WithTemperature sets the sampling temperature. When unset, the field is
// omitted and the model's default applies (Google recommends keeping it for
// Gemini 3).
func WithTemperature(temp float32) Option {
	return func(c *Client) {
		c.temperature = temp
		c.temperatureSet = true
	}
}

// WithMaxTokens sets the maximum number of output tokens. When unset, the
// model's limit applies.
func WithMaxTokens(maxTokens int32) Option {
	return func(c *Client) {
		if maxTokens > 0 {
			c.maxTokens = maxTokens
		}
	}
}

// WithThinkingLevel sets how much a Gemini 3 model thinks before answering.
func WithThinkingLevel(level ThinkingLevel) Option {
	return func(c *Client) {
		c.thinkingLevel = level
	}
}

// WithThinkingBudget sets the thinking budget in tokens, for models that take
// a budget rather than a level (0 disables thinking where the model allows
// it, -1 lets the model decide).
func WithThinkingBudget(tokens int32) Option {
	return func(c *Client) {
		c.thinkingBudget = &tokens
	}
}

// WithIncludeThoughts asks the model for summaries of its thinking, returned
// as reasoning parts (and as ReasoningDelta when streaming).
func WithIncludeThoughts() Option {
	return func(c *Client) {
		c.includeThoughts = true
	}
}
