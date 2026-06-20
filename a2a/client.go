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
	"fmt"
	"net/url"
	"time"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

const defaultPollingInterval = 500 * time.Millisecond

// ClientOption configures a [Client].
type ClientOption func(*clientConfig)

type clientConfig struct {
	resolver            *agentcard.Resolver
	pushConfig          *sdka2a.PushConfig
	acceptedOutputModes []string
	preferredTransports []sdka2a.TransportProtocol
	interceptors        []a2aclient.CallInterceptor
	pollingInterval     time.Duration
}

// WithResolver overrides the default [agentcard.Resolver] used to fetch the
// remote AgentCard.
func WithResolver(r *agentcard.Resolver) ClientOption {
	return func(c *clientConfig) { c.resolver = r }
}

// WithPushConfig sets the default push notification configuration applied to
// every task sent by this client.
func WithPushConfig(cfg *sdka2a.PushConfig) ClientOption {
	return func(c *clientConfig) { c.pushConfig = cfg }
}

// WithAcceptedOutputModes declares the MIME types the client can consume.
// Agents may use this to decide which output format to produce.
func WithAcceptedOutputModes(modes ...string) ClientOption {
	return func(c *clientConfig) { c.acceptedOutputModes = modes }
}

// WithPreferredTransports sets the ordered list of preferred transport protocols.
// The first protocol supported by both client and server will be selected.
func WithPreferredTransports(protocols ...sdka2a.TransportProtocol) ClientOption {
	return func(c *clientConfig) { c.preferredTransports = protocols }
}

// WithClientInterceptors registers one or more [a2aclient.CallInterceptor] values
// that run before and after every outgoing A2A call. Use this to inject
// authentication headers (e.g. Authorization: Bearer …), distributed tracing
// spans, or custom logging.
func WithClientInterceptors(interceptors ...a2aclient.CallInterceptor) ClientOption {
	return func(c *clientConfig) { c.interceptors = append(c.interceptors, interceptors...) }
}

// WithPollingInterval sets the interval used when polling GetTask for completion
// after a streaming subscription fails or is unavailable. Defaults to 500 ms.
// Non-positive values are ignored and the default is kept.
func WithPollingInterval(d time.Duration) ClientOption {
	return func(c *clientConfig) {
		if d > 0 {
			c.pollingInterval = d
		}
	}
}

// Client wraps a remote A2A agent and can expose it as an [llm.Tool].
type Client struct {
	card   *sdka2a.AgentCard
	client *a2aclient.Client
	cfg    *clientConfig
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
		resolver:        agentcard.DefaultResolver,
		pollingInterval: defaultPollingInterval,
	}

	for _, o := range opts {
		o(cfg)
	}

	card, err := cfg.resolver.Resolve(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	clientCfg := a2aclient.Config{
		AcceptedOutputModes: cfg.acceptedOutputModes,
		PreferredTransports: cfg.preferredTransports,
	}
	if cfg.pushConfig != nil {
		clientCfg.PushConfig = cfg.pushConfig
	}

	factoryOpts := []a2aclient.FactoryOption{a2aclient.WithConfig(clientCfg)}
	if len(cfg.interceptors) > 0 {
		factoryOpts = append(factoryOpts, a2aclient.WithCallInterceptors(cfg.interceptors...))
	}

	c, err := a2aclient.NewFromCard(ctx, card, factoryOpts...)
	if err != nil {
		return nil, err
	}

	return &Client{card: card, client: c, cfg: cfg}, nil
}

// Card returns the AgentCard resolved for the remote agent.
func (c *Client) Card() *sdka2a.AgentCard {
	return c.card
}

// AsTool converts the remote agent into an [llm.Tool] that a phero agent can call.
//
// The tool name and description are taken from the remote AgentCard. The tool
// handler sends a SendMessage request and waits for the task to reach a terminal
// state, transparently handling both synchronous (inline Message) and asynchronous
// (Task-based) responses. Async tasks are resolved via event subscription with a
// polling fallback.
//
// The tool input and output are text-only. Non-text parts in the remote agent's
// response (images, raw bytes) are not surfaced; if the response contains no text
// the tool returns [ErrNoTextContent].
// responses, but the logic is straightforward and well-commented.
//
//nolint:gocognit
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

		// If the server returned a non-terminal Task, wait until it completes.
		if task, ok := result.(*sdka2a.Task); ok && !task.Status.State.Terminal() {
			task, err = c.waitForTask(ctx, task)
			if err != nil {
				return nil, err
			}

			result = task
		}

		// Translate task failure/cancellation to errors.
		if task, ok := result.(*sdka2a.Task); ok {
			switch task.Status.State {
			case sdka2a.TaskStateFailed:
				if reason := extractStatusMessage(task.Status.Message); reason != "" {
					return nil, fmt.Errorf("%w: %s", ErrTaskFailed, reason)
				}

				return nil, ErrTaskFailed
			case sdka2a.TaskStateCanceled:
				return nil, ErrTaskCanceled
			case sdka2a.TaskStateCompleted, sdka2a.TaskStateUnspecified, sdka2a.TaskStateAuthRequired,
				sdka2a.TaskStateInputRequired, sdka2a.TaskStateRejected, sdka2a.TaskStateSubmitted,
				sdka2a.TaskStateWorking:
				// fall through to text extraction below
			}
		}

		text, err := extractTextFromResult(result)
		if err != nil {
			return nil, err
		}

		return &toolOutput{Output: text}, nil
	}

	return llm.NewTool(agent.SanitizeToolName(c.card.Name), c.card.Description, handler)
}

// extractStatusMessage returns the first text part from a task status message,
// or an empty string if the message is nil or contains no text parts.
func extractStatusMessage(msg *sdka2a.Message) string {
	if msg == nil {
		return ""
	}

	for _, part := range msg.Parts {
		if t := part.Text(); t != "" {
			return t
		}
	}

	return ""
}

// waitForTask blocks until the task reaches a terminal state and returns the
// final Task. It first attempts to subscribe to the task's event stream; if
// that fails or the server does not support streaming, it falls back to polling
// GetTask at the configured polling interval (default 500 ms).
//
//nolint:gocognit
func (c *Client) waitForTask(ctx context.Context, task *sdka2a.Task) (*sdka2a.Task, error) {
	if task.Status.State.Terminal() {
		return task, nil
	}

	// Try streaming subscription first (works when server supports streaming).
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	for event, err := range c.client.SubscribeToTask(subCtx, &sdka2a.SubscribeToTaskRequest{ID: task.ID}) {
		if err != nil {
			break
		}

		switch v := event.(type) {
		case *sdka2a.Task:
			if v.Status.State.Terminal() {
				return v, nil
			}
		case *sdka2a.TaskStatusUpdateEvent:
			if v.Status.State.Terminal() {
				t, getErr := c.client.GetTask(ctx, &sdka2a.GetTaskRequest{ID: task.ID})
				if getErr != nil {
					return nil, getErr
				}

				return t, nil
			}
		}
	}

	// Fall back to polling.
	ticker := time.NewTicker(c.cfg.pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			t, err := c.client.GetTask(ctx, &sdka2a.GetTaskRequest{ID: task.ID})
			if err != nil {
				return nil, err
			}

			if t.Status.State.Terminal() {
				return t, nil
			}
		}
	}
}
