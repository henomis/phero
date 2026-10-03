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
	// Session is the optional metadata.session value — the session the agent was
	// served with, not the instance name it produces. Filter on it with
	// [FilterBySession].
	Session string
	// Name is the instance name — the 5th token of the prompt endpoint subject.
	// For a sessioned agent that is [InstanceName](name, session).
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

// Send sends req to this agent. It delegates to [Client.Send].
func (h *AgentHandle) Send(ctx context.Context, req *Request) (*Stream, error) {
	return h.client.Send(ctx, &h.AgentInfo, req)
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
// responding compliant agent instances. An instance whose protocol_version this
// package cannot speak (see §11: within 0.x the MAJOR.MINOR must match exactly)
// is skipped like any other non-compliant reply.
//
// It uses a stall strategy: collection ends after 750 ms of silence from the
// last response, capped by a 2 s absolute deadline (both configurable via
// [WithDiscoveryTimeout]). Any DiscoverOption filters are applied client-side.
//
// ctx bounds the collection: an earlier deadline wins over the discovery
// timeout, and a cancellation ends it immediately. That matters most during a
// shutdown, where a resolve that ignored its context would spend the caller's
// whole drain budget waiting for replies nobody is going to use.
//
// Returns [ErrNoAgentsFound] if the filtered result set is empty, or ctx's error
// if the context ended before anything was collected.
func (c *Client) Discover(ctx context.Context, opts ...DiscoverOption) ([]*AgentHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	var infos []*AgentHandle

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		timeout := min(c.cfg.stallTimeout, remaining)

		msgCtx, cancel := context.WithTimeout(ctx, timeout)
		msg, msgErr := sub.NextMsgWithContext(msgCtx)

		cancel()

		if msgErr != nil {
			break // stall, deadline or cancellation — stop collecting
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
		// A cancelled context is a different answer from "nobody is there", and
		// a caller retrying on ErrNoAgentsFound must not spin on a dead context.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		return nil, ErrNoAgentsFound
	}

	return infos, nil
}

// Prompt sends a plain-text prompt to the agent described by info and returns
// a [Stream] for consuming the streamed response.  The caller must call
// [Stream.Close] when done. It is [Client.Send] with only a prompt.
func (c *Client) Prompt(ctx context.Context, info *AgentInfo, text string) (*Stream, error) {
	return c.Send(ctx, info, &Request{Prompt: text})
}

// Send sends req — a prompt, with optional attachments and headers — to the
// agent described by info and returns a [Stream] for consuming the streamed
// response.  The caller must call [Stream.Close] when done.
//
// The request is checked before anything is published (§5.4): an empty prompt,
// attachments the agent does not accept, an attachment without a filename, and
// a request larger than the agent's or the connection's max_payload all fail
// here, with an error [Permanent] reports as permanent.
//
// An already-ended ctx returns its error without publishing: the send itself
// does not block, but starting work on behalf of a call that is already over
// means an agent runs — and bills — a prompt with nobody left to read it.
// Reading the reply is bounded by the ctx passed to [Stream.Text].
func (c *Client) Send(ctx context.Context, info *AgentInfo, req *Request) (*Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	msg, err := c.requestMsg(info, req)
	if err != nil {
		return nil, err
	}

	sub, err := c.nc.SubscribeSync(msg.Reply)
	if err != nil {
		return nil, fmt.Errorf("nats: subscribe reply: %w", err)
	}

	if pubErr := c.nc.PublishMsg(msg); pubErr != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("nats: publish prompt: %w", pubErr)
	}

	return &Stream{
		sub:               sub,
		inactivityTimeout: c.cfg.inactivityTimeout,
	}, nil
}

// requestMsg validates req against info and the connection (§5.4) and builds
// the message that carries it, with a fresh inbox as its reply subject.
func (c *Client) requestMsg(info *AgentInfo, req *Request) (*natsclient.Msg, error) {
	if req == nil || strings.TrimSpace(req.Prompt) == "" {
		return nil, ErrEmptyPrompt
	}

	if len(req.Attachments) > 0 && !info.AttachmentsOk {
		return nil, ErrAttachmentsNotAllowed
	}

	for _, a := range req.Attachments {
		if strings.TrimSpace(a.Filename) == "" {
			return nil, ErrInvalidAttachment
		}
	}

	body, err := json.Marshal(req.envelope())
	if err != nil {
		return nil, fmt.Errorf("nats: encode prompt: %w", err)
	}

	msg := &natsclient.Msg{Subject: info.PromptSubject, Reply: c.nc.NewInbox(), Header: req.Header, Data: body}

	// Both caps, because they are different facts. The advertised one is the
	// agent's policy; the connection's is what the transport will actually
	// carry, and exceeding it fails at publish as `nats: maximum payload
	// exceeded` — the opaque error ErrPayloadTooLarge exists to replace. The
	// comparison is exact: nats.go rejects on len(data)+len(headers), and
	// Msg.Size measures the headers as nats.go encodes them.
	size := int64(msg.Size() - len(msg.Subject) - len(msg.Reply))

	if info.MaxPayloadBytes > 0 && size > info.MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}

	if connLimit := c.nc.MaxPayload(); connLimit > 0 && size > connLimit {
		return nil, fmt.Errorf("%w: %d bytes exceeds this connection's max_payload (%d bytes)",
			ErrPayloadTooLarge, size, connLimit)
	}

	return msg, nil
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

	// next, when set, replaces nextMsg: tests use it to script a stream
	// without a broker.
	next func(ctx context.Context) (*natsclient.Msg, error)
}

// Text reads all response chunks from the stream and returns the concatenated
// text.  It returns [ErrStreamTimeout] when no message arrives within the
// inactivity timeout (§6.6).  The stream is automatically drained on return.
//
// If the agent asks a question mid-stream (§7), Text returns a *[QueryError]
// carrying it: this client cannot answer, and waiting would only end in a
// timeout. Text discards any text received before the error, as it does for
// a service error.
func (s *Stream) Text(ctx context.Context) (string, error) {
	defer s.sub.Unsubscribe() //nolint:errcheck

	next := s.nextMsg
	if s.next != nil {
		next = s.next
	}

	var sb strings.Builder

	for {
		msg, err := next(ctx)
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

		switch chunk.Type {
		case chunkTypeResponse:
			sb.WriteString(decodeResponseText(chunk.Data))
		case chunkTypeQuery:
			return "", parseQuery(chunk.Data)
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

	if !compatibleProtocol(svc.Metadata[metaProtocolVersion]) {
		return nil
	}

	info := &AgentInfo{
		InstanceID:      svc.ID,
		Agent:           svc.Metadata[metaAgent],
		Owner:           svc.Metadata[metaOwner],
		Session:         svc.Metadata[metaSession],
		ProtocolVersion: svc.Metadata[metaProtocolVersion],
	}

	for _, ep := range svc.Endpoints {
		switch ep.Name {
		case endpointPrompt:
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

// compatibleProtocol reports whether an agent advertising protocol version v
// can be prompted by this package, which implements [protocolVersion] (§11).
//
// Only the MAJOR.MINOR prefix carries meaning; patch and pre-release qualifiers
// ("0.3.1", "0.3-rc1") are ignored (§11.1). Different MAJOR versions have no
// interoperability guarantee. Within 0.x a MINOR bump may break the wire — 0.2
// used a different subject hierarchy — so callers pin the exact MAJOR.MINOR
// until 1.0 (§11.2). From 1.0, a different MINOR of the same MAJOR is compatible.
func compatibleProtocol(v string) bool {
	major, minor, ok := majorMinor(v)
	if !ok {
		return false
	}

	ourMajor, ourMinor, _ := majorMinor(protocolVersion)

	if major != ourMajor {
		return false
	}

	return major != "0" || minor == ourMinor
}

// majorMinor extracts the MAJOR and MINOR numbers of a version string.
func majorMinor(v string) (major, minor string, ok bool) {
	major, rest, found := strings.Cut(v, ".")
	if !found || !isDigits(major) {
		return "", "", false
	}

	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}

	if end == 0 {
		return "", "", false
	}

	return major, rest[:end], true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
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

	if f.session != "" && info.Session != f.session {
		return false
	}

	return true
}
