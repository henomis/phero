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

package openai

import (
	"context"

	"github.com/sashabaranov/go-openai"

	"github.com/henomis/phero/llm"
)

// Client implements the llm.LLM interface using the OpenAI API.
var _ llm.LLM = (*Client)(nil)

const (
	// DefaultModel is the model used when no explicit model option is provided.
	DefaultModel = "gpt-4o-mini"
	// OllamaBaseURL is the OpenAI-compatible base URL used by the local Ollama server.
	OllamaBaseURL = "http://localhost:11434/v1"
)

// Client is an llm.LLM implementation that uses github.com/sashabaranov/go-openai.
type Client struct {
	client *openai.Client

	model       string
	apiKey      string
	temperature float32
	config      openai.ClientConfig
}

// Option configures a Client created by New.
type Option func(*Client)

// New constructs a new Client with the given API key and applies any options.
//
// By default it uses DefaultModel and the standard OpenAI base URL from
// go-openai's DefaultConfig.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey: apiKey,
		model:  DefaultModel,
		config: openai.DefaultConfig(apiKey),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	c.client = openai.NewClientWithConfig(c.config)
	return c
}

// Execute calls the Chat Completions API with the given messages and returns the
// model's next message.
func (c *Client) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	request := openai.ChatCompletionRequest{
		Model:       c.model,
		Messages:    messages,
		Temperature: c.temperature,
	}

	if len(tools) > 0 {
		request.Tools = c.openaiTools(tools)
	}

	response, err := c.client.CreateChatCompletion(ctx, request)
	if err != nil {
		return nil, err
	}

	if len(response.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	return &llm.Result{
		Message: &response.Choices[0].Message,
		Usage: &llm.Usage{
			InputTokens:  response.Usage.PromptTokens,
			OutputTokens: response.Usage.CompletionTokens,
		},
	}, nil
}

func (c *Client) openaiTools(tools []*llm.Tool) []openai.Tool {
	openaiTools := make([]openai.Tool, len(tools))
	for i, tool := range tools {
		openaiTools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name(),
				Parameters:  tool.InputSchema(),
				Description: tool.Description(),
				Strict:      true, // default to strict JSON schema for better performance and reliability
			},
		}
	}

	return openaiTools
}

// WithBaseURL sets the base URL used by the underlying OpenAI client.
//
// This enables use with OpenAI-compatible endpoints.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.config.BaseURL = baseURL
		}
	}
}

// WithModel sets the model name used for chat completions.
func WithModel(model string) Option {
	return func(c *Client) {
		c.model = model
	}
}

// WithOllamaBaseURL configures the client to use the default local Ollama base URL.
func WithOllamaBaseURL() Option {
	return WithBaseURL(OllamaBaseURL)
}

// WithTemperature sets the sampling temperature used for chat completions.
func WithTemperature(temp float32) Option {
	return func(c *Client) {
		c.temperature = temp
	}
}
