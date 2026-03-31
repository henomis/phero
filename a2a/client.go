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

package a2a

import (
	"context"
	"net/url"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	"github.com/henomis/phero/llm"
)

// ClientOption configures a [Client].
type ClientOption func(*clientConfig)

type clientConfig struct {
	resolver *agentcard.Resolver
}

// WithResolver overrides the default [agentcard.Resolver] used to fetch the
// remote AgentCard.
func WithResolver(r *agentcard.Resolver) ClientOption {
	return func(c *clientConfig) {
		c.resolver = r
	}
}

// Client wraps a remote A2A agent and can expose it as an [llm.Tool].
type Client struct {
	card   *sdka2a.AgentCard
	client *a2aclient.Client
}

// NewClient resolves the AgentCard at baseURL and creates a transport-agnostic
// A2A client.
//
// baseURL should be the root URL of the remote agent server (e.g.
// "http://localhost:8080"). The well-known agent card path is appended
// automatically by the resolver.
func NewClient(ctx context.Context, baseURL string, opts ...ClientOption) (*Client, error) {
	if baseURL == "" {
		return nil, ErrURLRequired
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, ErrInvalidBaseURL
	}

	cfg := &clientConfig{
		resolver: agentcard.DefaultResolver,
	}

	for _, o := range opts {
		o(cfg)
	}

	card, err := cfg.resolver.Resolve(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	c, err := a2aclient.NewFromCard(ctx, card)
	if err != nil {
		return nil, err
	}

	return &Client{card: card, client: c}, nil
}

// AsTool converts the remote agent into an [llm.Tool] that a phero agent can
// call.
//
// The tool name and description are taken from the remote AgentCard.
func (c *Client) AsTool() (*llm.Tool, error) {
	type toolInput struct {
		Input string `json:"input" jsonschema:"description=Instructions or question for the remote agent."`
	}

	type toolOutput struct {
		Output string `json:"output" jsonschema:"description=The remote agent's response."`
	}

	handler := func(ctx context.Context, in *toolInput) (*toolOutput, error) {
		msg := sdka2a.NewMessage(sdka2a.MessageRoleUser, sdka2a.NewTextPart(in.Input))

		result, err := c.client.SendMessage(ctx, &sdka2a.SendMessageRequest{Message: msg})
		if err != nil {
			return nil, err
		}

		if result == nil {
			return nil, ErrEmptyResponse
		}

		text, err := extractTextFromResult(result)
		if err != nil {
			return nil, err
		}

		return &toolOutput{Output: text}, nil
	}

	return llm.NewTool(c.card.Name, c.card.Description, handler)
}

// extractTextFromResult extracts the first text content from a SendMessageResult.
//
// A result is either a *sdka2a.Message (when the agent responds inline) or a
// *sdka2a.Task (when the server creates a task to track the work). Both cases
// are handled here.
func extractTextFromResult(result sdka2a.SendMessageResult) (string, error) {
	switch v := result.(type) {
	case *sdka2a.Message:
		for _, part := range v.Parts {
			if t := part.Text(); t != "" {
				return t, nil
			}
		}

		return "", ErrNoTextContent

	case *sdka2a.Task:
		if v.Status.Message != nil {
			for _, part := range v.Status.Message.Parts {
				if t := part.Text(); t != "" {
					return t, nil
				}
			}
		}

		return "", ErrNoTextContent

	default:
		return "", ErrNoTextContent
	}
}
