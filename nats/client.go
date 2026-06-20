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

package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	natsclient "github.com/nats-io/nats.go"

	"github.com/henomis/phero/llm"
)

// natsSubjectParts is the number of dot-separated tokens in a verb-first NATS subject:
// agents.{verb}.{agent}.{owner}.{name}.
const natsSubjectParts = 5

// AgentInfo holds the parsed discovery record for a single agent instance.
type AgentInfo struct {
	// InstanceID is the framework-assigned per-instance identifier (§3.4).
	InstanceID string
	// Agent is the metadata.agent value (e.g. "phero", "claude-code").
	Agent string
	// Owner is the metadata.owner value.
	Owner string
	// Session is the optional metadata.session value.
	Session string
	// Name is the instance name — the 5th token of the prompt endpoint subject.
	Name string
	// ProtocolVersion is the metadata.protocol_version value.
	ProtocolVersion string
	// PromptSubject is the subject to publish prompt requests to (§4.3).
	PromptSubject string
	// StatusSubject is the on-demand status request subject (§8.7).
	StatusSubject string
	// MaxPayloadBytes is the parsed max_payload endpoint metadata (§2.1).
	MaxPayloadBytes int64
	// AttachmentsOk mirrors the attachments_ok endpoint metadata flag (§2.1).
	AttachmentsOk bool
}

// AgentHandle is the live handle returned by [Client.Discover]. It bundles the
// discovery data ([AgentInfo]) with the [Client] that found it, so callers can
// call Prompt and AsTool without threading the client separately.
type AgentHandle struct {
	AgentInfo

	client *Client
}

// Prompt sends a plain-text prompt to this agent and returns a [Stream] for
// consuming the streamed response. It delegates to [Client.Prompt].
func (h *AgentHandle) Prompt(ctx context.Context, text string) (*Stream, error) {
	return h.client.Prompt(ctx, &h.AgentInfo, text)
}

// AsTool wraps this agent as an [llm.Tool]. It delegates to [Client.AsTool].
func (h *AgentHandle) AsTool(toolName, toolDesc string) (*llm.Tool, error) {
	return h.client.AsTool(&h.AgentInfo, toolName, toolDesc)
}

// Client discovers and prompts NATS Agent Protocol agents.
type Client struct {
	nc  *natsclient.Conn
	cfg *clientConfig
}

// NewClient creates a Client using an established NATS connection.
func NewClient(nc *natsclient.Conn, opts ...ClientOption) *Client {
	cfg := defaultClientConfig()

	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	return &Client{nc: nc, cfg: cfg}
}

// Discover sends a $SRV.INFO.agents fan-out request and collects all
// responding compliant agent instances.
//
// It uses a stall strategy: collection ends after 750 ms of silence from the
// last response, capped by a 2 s absolute deadline (both configurable via
// [WithDiscoveryTimeout]). Any DiscoverOption filters are applied client-side.
//
// Returns [ErrNoAgentsFound] if the filtered result set is empty.
func (c *Client) Discover(_ context.Context, opts ...DiscoverOption) ([]*AgentHandle, error) {
	filter := &discoverFilter{}

	for _, opt := range opts {
		if opt != nil {
			opt(filter)
		}
	}

	inbox := c.nc.NewInbox()

	sub, err := c.nc.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("nats: discovery subscribe: %w", err)
	}
	defer sub.Unsubscribe() //nolint:errcheck

	if pubErr := c.nc.PublishRequest("$SRV.INFO.agents", inbox, nil); pubErr != nil {
		return nil, fmt.Errorf("nats: discovery publish: %w", pubErr)
	}

	deadline := time.Now().Add(c.cfg.discoveryTimeout)

	var infos []*AgentHandle

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		timeout := min(c.cfg.stallTimeout, remaining)

		msg, msgErr := sub.NextMsg(timeout)
		if msgErr != nil {
			break // stall or deadline — stop collecting
		}

		info := parseAgentInfo(msg.Data)
		if info == nil {
			continue
		}

		if !matchFilter(info, filter) {
			continue
		}

		infos = append(infos, &AgentHandle{AgentInfo: *info, client: c})
	}

	if len(infos) == 0 {
		return nil, ErrNoAgentsFound
	}

	return infos, nil
}

// Prompt sends a plain-text prompt to the agent described by info and returns
// a [Stream] for consuming the streamed response.  The caller must call
// [Stream.Close] when done.
func (c *Client) Prompt(_ context.Context, info *AgentInfo, text string) (*Stream, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyPrompt
	}

	env := envelope{Prompt: text}

	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("nats: encode prompt: %w", err)
	}

	if info.MaxPayloadBytes > 0 && int64(len(body)) > info.MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}

	inbox := c.nc.NewInbox()

	sub, err := c.nc.SubscribeSync(inbox)
	if err != nil {
		return nil, fmt.Errorf("nats: subscribe reply: %w", err)
	}

	if pubErr := c.nc.PublishRequest(info.PromptSubject, inbox, body); pubErr != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("nats: publish prompt: %w", pubErr)
	}

	return &Stream{
		sub:               sub,
		inactivityTimeout: c.cfg.inactivityTimeout,
	}, nil
}

// AsTool wraps a remote NATS agent as an [llm.Tool] that any local Phero
// agent can call.  The tool's input schema has a single "Prompt" string field.
func (c *Client) AsTool(info *AgentInfo, toolName, toolDesc string) (*llm.Tool, error) {
	type input struct {
		Prompt string `json:"prompt" jsonschema:"The prompt text to send to the remote agent."`
	}

	return llm.NewTool(toolName, toolDesc,
		func(ctx context.Context, args input) (string, error) {
			stream, err := c.Prompt(ctx, info, args.Prompt)
			if err != nil {
				return "", err
			}
			defer stream.Close()

			return stream.Text(ctx)
		},
	)
}

// Stream consumes the chunked response from a prompt request.
type Stream struct {
	sub               *natsclient.Subscription
	inactivityTimeout time.Duration
}

// Text reads all response chunks from the stream and returns the concatenated
// text.  It returns [ErrStreamTimeout] when no message arrives within the
// inactivity timeout (§6.6).  The stream is automatically drained on return.
func (s *Stream) Text(ctx context.Context) (string, error) {
	defer s.sub.Unsubscribe() //nolint:errcheck

	var sb strings.Builder

	for {
		msg, err := s.nextMsg(ctx)
		if err != nil {
			if errors.Is(err, natsclient.ErrTimeout) {
				return "", ErrStreamTimeout
			}

			return "", err
		}

		if isTerminator(msg) {
			return sb.String(), nil
		}

		if isServiceError(msg) {
			return "", parseServiceError(msg)
		}

		var chunk rawChunk
		if unmarshalErr := json.Unmarshal(msg.Data, &chunk); unmarshalErr != nil {
			continue // §6.6: silently ignore unknown or unparseable chunks
		}

		if chunk.Type == chunkTypeResponse {
			sb.WriteString(decodeResponseText(chunk.Data))
		}
		// "status" ack chunks and unknown types are silently ignored (§6.4, §6.6).
	}
}

// Close cancels the stream by unsubscribing from the reply subject (§6.7).
// After Close, callers must not call Text again.
func (s *Stream) Close() {
	_ = s.sub.Unsubscribe()
}

// nextMsg waits for the next message with a per-call inactivity deadline that
// respects ctx cancellation. Each call resets the inactivity window, implementing
// the "since the last observed chunk" semantics required by §6.6.
func (s *Stream) nextMsg(ctx context.Context) (*natsclient.Msg, error) {
	deadline := time.Now().Add(s.inactivityTimeout)
	tctx, tcancel := context.WithDeadline(ctx, deadline)
	msg, err := s.sub.NextMsgWithContext(tctx)

	tcancel()

	return msg, err
}

// — Helpers ————————————————————————————————————————————————————————————————

// parseAgentInfo extracts an AgentInfo from a raw $SRV.INFO.agents response
// body.  Returns nil if the body is not a compliant agent.
func parseAgentInfo(data []byte) *AgentInfo {
	var svc serviceInfoResponse
	if err := json.Unmarshal(data, &svc); err != nil {
		return nil
	}

	if svc.Name != svcNameAgents {
		return nil
	}

	if svc.Metadata["protocol_version"] == "" {
		return nil
	}

	info := &AgentInfo{
		InstanceID:      svc.ID,
		Agent:           svc.Metadata["agent"],
		Owner:           svc.Metadata["owner"],
		Session:         svc.Metadata["session"],
		ProtocolVersion: svc.Metadata["protocol_version"],
	}

	for _, ep := range svc.Endpoints {
		switch ep.Name {
		case "prompt":
			info.PromptSubject = ep.Subject
			if v := ep.Metadata["max_payload"]; v != "" {
				if n, err := parseMaxPayload(v); err == nil {
					info.MaxPayloadBytes = n
				}
			}

			if v := ep.Metadata["attachments_ok"]; v == attachmentsOkTrue {
				info.AttachmentsOk = true
			}

			info.Name = instanceNameFromSubject(ep.Subject)
		case chunkTypeStatus:
			info.StatusSubject = ep.Subject
		}
	}

	if info.PromptSubject == "" {
		return nil // no prompt endpoint — not compliant
	}

	return info
}

// instanceNameFromSubject extracts the 5th token from a verb-first subject
// agents.{verb}.{agent}.{owner}.{name}.
func instanceNameFromSubject(subject string) string {
	parts := strings.SplitN(subject, ".", natsSubjectParts)
	if len(parts) == natsSubjectParts {
		return parts[4]
	}

	return ""
}

// matchFilter returns true if info passes all non-empty filter fields.
func matchFilter(info *AgentInfo, f *discoverFilter) bool {
	if f.agent != "" && info.Agent != f.agent {
		return false
	}

	if f.owner != "" && info.Owner != f.owner {
		return false
	}

	if f.name != "" && info.Name != f.name {
		return false
	}

	return true
}
