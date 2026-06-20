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
	"fmt"
	"sync"
	"time"

	natsclient "github.com/nats-io/nats.go"
	natsio "github.com/nats-io/nats.go/micro"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

const protocolVersion = "0.3"

// Handler is implemented by anything that can process a prompt — *agent.Agent
// and workflow executors both satisfy it.
type Handler interface {
	Run(ctx context.Context, parts ...llm.ContentPart) (*agent.Result, error)
}

// Server registers a Handler as a NATS micro service implementing the
// NATS Agent Protocol v0.3. It handles:
//
//   - Service registration and discovery via $SRV.PING/INFO.agents (§3, §4).
//   - Streaming prompt responses on the prompt endpoint (§5, §6).
//   - Heartbeat publication on agents.hb.{agent}.{owner}.{name} (§8).
//   - On-demand status replies on the status endpoint (§8.7).
type Server struct {
	nc      *natsclient.Conn
	handler Handler
	cfg     *serverConfig
	owner   string
	name    string

	svc natsio.Service
	wg  sync.WaitGroup
}

// New creates a Server that wraps h and serves it on NATS.
// owner and name are required positional arguments (§3.2):
//   - owner identifies the operator or account.
//   - name is the per-instance label (the 5th token in the subject hierarchy).
func New(nc *natsclient.Conn, h Handler, owner, name string, opts ...ServerOption) (*Server, error) {
	if nc == nil {
		return nil, ErrNilConn
	}

	if h == nil {
		return nil, ErrNilHandler
	}

	if owner == "" {
		return nil, ErrEmptyOwner
	}

	if name == "" {
		return nil, ErrEmptyName
	}

	cfg := defaultServerConfig()

	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if cfg.session != "" {
		name = name + "-" + cfg.session
		cfg.session = name
	}

	return &Server{
		nc:      nc,
		handler: h,
		cfg:     cfg,
		owner:   owner,
		name:    name,
	}, nil
}

// Start registers the NATS micro service, begins publishing heartbeats, and
// blocks until ctx is cancelled.  Call Stop to drain subscriptions after Start
// returns, or cancel ctx and let Start handle cleanup automatically.
func (s *Server) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	attachmentsOkStr := "false"
	if s.cfg.attachmentsOk {
		attachmentsOkStr = "true"
	}

	metadata := map[string]string{
		"agent":            s.cfg.agentID,
		"owner":            s.owner,
		"protocol_version": protocolVersion,
	}
	if s.cfg.session != "" {
		metadata["session"] = s.cfg.session
	}

	svc, err := natsio.AddService(s.nc, natsio.Config{
		Name:        "agents",
		Version:     s.cfg.version,
		Description: fmt.Sprintf("%s/%s — %s", s.cfg.agentID, s.owner, s.name),
		Metadata:    metadata,
	})
	if err != nil {
		return fmt.Errorf("nats: register micro service: %w", err)
	}

	s.svc = svc

	promptSubject := fmt.Sprintf("agents.prompt.%s.%s.%s", s.cfg.agentID, s.owner, s.name)
	statusSubject := fmt.Sprintf("agents.status.%s.%s.%s", s.cfg.agentID, s.owner, s.name)
	hbSubject := fmt.Sprintf("agents.hb.%s.%s.%s", s.cfg.agentID, s.owner, s.name)

	if addErr := svc.AddEndpoint("prompt",
		natsio.ContextHandler(ctx, s.handlePrompt),
		natsio.WithEndpointSubject(promptSubject),
		natsio.WithEndpointQueueGroup("agents"),
		natsio.WithEndpointMetadata(map[string]string{
			"max_payload":    s.cfg.maxPayload,
			"attachments_ok": attachmentsOkStr,
		}),
	); addErr != nil {
		_ = svc.Stop()
		return fmt.Errorf("nats: register prompt endpoint: %w", addErr)
	}

	if addErr := svc.AddEndpoint("status",
		natsio.ContextHandler(ctx, s.handleStatus),
		natsio.WithEndpointSubject(statusSubject),
		natsio.WithEndpointQueueGroup("agents"),
	); addErr != nil {
		_ = svc.Stop()
		return fmt.Errorf("nats: register status endpoint: %w", addErr)
	}

	instanceID := svc.Info().ID

	s.wg.Go(func() { s.startHeartbeats(ctx, hbSubject, instanceID) })

	<-ctx.Done()

	_ = svc.Stop()

	s.wg.Wait()

	return nil
}

// Stop is a convenience method that cancels any Start context and drains
// in-flight prompt handlers.  It is a no-op if Start has not been called or
// has already returned.
func (s *Server) Stop() {
	s.wg.Wait()
}

// handlePrompt dispatches each prompt request into a goroutine so that the
// NATS subscription callback returns immediately and the subscription remains
// responsive to further requests (§3.4).
func (s *Server) handlePrompt(ctx context.Context, req natsio.Request) {
	s.wg.Go(func() { s.processPrompt(ctx, req) })
}

// processPrompt decodes the envelope, invokes the agent, streams the result,
// and always terminates the response stream with the zero-byte terminator (§6.5).
func (s *Server) processPrompt(ctx context.Context, req natsio.Request) {
	sendErr := func(code, errCode, message string) {
		_ = req.Error(code, message, encodeErrorBody(errCode, message))
		_ = req.Respond(nil) // terminator (§9.3)
	}

	env, err := decodeEnvelope(req.Data())
	if err != nil {
		sendErr("400", "malformed_envelope", err.Error())
		return
	}

	if !s.cfg.attachmentsOk && len(env.Attachments) > 0 {
		sendErr("400", "attachments_not_allowed", "this agent does not accept attachments")
		return
	}

	parts, err := envelopeToContentParts(env)
	if err != nil {
		sendErr("400", "malformed_envelope", err.Error())
		return
	}

	// Mandatory first message: ack before any latency-inducing work (§6.4).
	_ = req.Respond(encodeStatusChunk("ack"))

	// Keepalive: emit periodic ack chunks so the caller's inactivity timeout
	// does not fire during long-running agent work (§6.4).
	kaCtx, kaCancel := context.WithCancel(ctx)

	var kaWg sync.WaitGroup
	kaWg.Go(func() {
		ticker := time.NewTicker(s.cfg.keepaliveInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_ = req.Respond(encodeStatusChunk("ack"))
			case <-kaCtx.Done():
				return
			}
		}
	})

	result, runErr := s.handler.Run(ctx, parts...)

	// Stop keepalive before emitting the response/terminator so ack chunks
	// never race the terminator or the response chunk.
	kaCancel()
	kaWg.Wait()

	if runErr != nil {
		sendErr("500", "internal_error", runErr.Error())
		return
	}

	_ = req.Respond(encodeResponseChunk(result.TextContent()))
	_ = req.Respond(nil) // terminator (§6.5)
}

// handleStatus replies with a heartbeat-shaped JSON payload (§8.7).
// The request body is ignored per the spec.
func (s *Server) handleStatus(ctx context.Context, req natsio.Request) {
	instanceID := ""
	if s.svc != nil {
		instanceID = s.svc.Info().ID
	}

	p := heartbeatPayload{
		Agent:      s.cfg.agentID,
		Owner:      s.owner,
		Session:    s.cfg.session,
		InstanceID: instanceID,
		Ts:         time.Now().UTC().Format(time.RFC3339),
		IntervalS:  int(s.cfg.heartbeatInterval.Seconds()),
	}
	_ = req.Respond(encodeHeartbeat(p))
}
