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
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	natsclient "github.com/nats-io/nats.go"
	natsio "github.com/nats-io/nats.go/micro"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

const protocolVersion = "0.3"

// drainingMessage is the description returned to a prompt that arrives after
// the server has stopped accepting work.
const drainingMessage = "server is draining"

// errCodeServerDraining is the §9.1 machine-readable code of a drain refusal.
const errCodeServerDraining = "server_draining"

// §9.1 machine-readable codes of a response that could not be sent.
const (
	errCodeResponseTooLarge = "response_too_large"
	errCodeResponseFailed   = "response_failed"
)

// natsDefaultMaxPayload is NATS's default max_payload (1MB), assumed when the
// connection does not report its own.
const natsDefaultMaxPayload = 1 << 20

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

	// gate admits prompt handlers while the server is serving and counts the
	// ones in flight, so a drain can stop admitting and then wait. hbWG tracks
	// the heartbeat publisher, which is bound to the serving lifetime rather
	// than to the prompts in flight.
	gate *gate
	hbWG sync.WaitGroup

	// mu guards the handles Start publishes for a drain to act on. Drain may be
	// called from any goroutine, including before Start has registered anything.
	mu            sync.Mutex
	svc           natsio.Service
	stopServing   context.CancelFunc
	cancelPrompts context.CancelFunc
	// draining is set, under mu, in the same step that a drain snapshots the
	// handles above. A Start that finds it set must not publish a handle the
	// drain has already missed.
	draining bool

	drainOnce sync.Once
	drained   chan struct{}
	drainErr  error
}

// InstanceName returns the name an agent is registered under: its own name,
// suffixed with the session when [WithSession] is set. An empty session yields
// the name unchanged.
//
// It is exported because the rule is shared. The server composes the name; a
// client that addresses that agent by name — via [FilterByName], or by building
// the prompt subject itself — has to reproduce it, and a private join character
// would be a secret two codebases had to agree on by convention. Prefer
// [FilterBySession] where you can: it needs no composition at all.
func InstanceName(name, session string) string {
	if session == "" {
		return name
	}

	return name + "-" + session
}

// New creates a Server that wraps h and serves it on NATS.
// owner and name are required positional arguments (§3.2):
//   - owner identifies the operator or account.
//   - name is the per-instance label (the 5th token in the subject hierarchy).
//
// With [WithSession] set, the agent registers as [InstanceName](name, session)
// and advertises the session itself as metadata.session (§3.2).
//
// owner, name, the session and the agent id all become tokens of the subjects
// this Server subscribes, so each must satisfy [ValidateSubjectToken]; New
// returns [ErrInvalidSubjectToken] otherwise. This is not cosmetic — see that
// function for what an owner of "*" does to a shared queue group.
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

	// The session suffixes the registered instance name; it does *not* become
	// it. Overwriting cfg.session here would put the instance name into every
	// place the session is advertised — the service metadata, each heartbeat and
	// the status reply — so an agent "worker" with session "prod" would report
	// session="worker-prod", and nothing could group instances by session.
	name = InstanceName(name, cfg.session)

	if err := validateSubjectIdentity(cfg.agentID, owner, name, cfg.session); err != nil {
		return nil, err
	}

	if err := reconcileMaxPayload(cfg, nc.MaxPayload()); err != nil {
		return nil, err
	}

	return &Server{
		nc:      nc,
		handler: h,
		cfg:     cfg,
		owner:   owner,
		name:    name,
		gate:    newGate(),
		drained: make(chan struct{}),
	}, nil
}

// Start registers the NATS micro service, begins publishing heartbeats, and
// blocks until ctx is cancelled — at which point it drains (see [Server.Drain])
// rather than returning immediately.
//
// Cancelling ctx therefore means "shut down", not "abandon what you are doing":
// the prompt endpoint is unsubscribed so no further work is admitted, and the
// handlers already running keep their own context and finish. Start returns once
// they have, or [ErrDrainIncomplete] if the drain budget (see
// [WithDrainTimeout]) was spent and cancelling them did not make them return.
//
// A Server serves once. Start on a Server that has already drained returns
// [ErrServerStopped].
func (s *Server) Start(ctx context.Context) error {
	if s.hasDrained() {
		return ErrServerStopped
	}

	// Two lifetimes, deliberately. serveCtx is the server's: cancelling it stops
	// registration and heartbeats. promptCtx is the work's, and a shutdown does
	// not cancel it — a handler in the middle of an LLM call has already been
	// paid for, and killing it throws the answer away without refunding the
	// tokens. The drain cancels promptCtx only once its budget is spent.
	serveCtx, stopServing := context.WithCancel(ctx)
	defer stopServing()

	promptCtx, cancelPrompts := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelPrompts()

	// Registration is atomic with respect to a drain. runDrain snapshots the
	// handles under s.mu, so holding it from the draining check through the last
	// handle means the drain sees either nothing or everything. Without that, a
	// drain landing mid-registration stops a service that Start then adds
	// endpoints to — micro subscribes them regardless, and nothing ever
	// unsubscribes them — or Waits on hbWG while Start is adding to it. None of
	// the calls below waits on a round-trip, so the hold is short.
	s.mu.Lock()
	if s.draining {
		s.mu.Unlock()
		return ErrServerStopped
	}

	s.stopServing = stopServing
	s.cancelPrompts = cancelPrompts

	svc, err := s.register(serveCtx, promptCtx)
	if err == nil {
		s.svc = svc
	}
	s.mu.Unlock()

	if err != nil {
		return err
	}

	<-serveCtx.Done()

	// ctx is done — that is the shutdown signal — so waiting on it would end the
	// wait before it began. WithoutCancel keeps its values and drops both its
	// cancellation and its deadline: a deadline is just another way ctx ends,
	// and when it is what fired, it has already passed. The drain is bounded by
	// WithDrainTimeout instead. A caller who wants a tighter bound on its own
	// wait should call Drain with a context of its own.
	return s.Drain(context.WithoutCancel(ctx))
}

// promptSubject is the subject the prompt endpoint serves on (§2).
func (s *Server) promptSubject() string {
	return fmt.Sprintf("agents.prompt.%s.%s.%s", s.cfg.agentID, s.owner, s.name)
}

// promptMetadata is the prompt endpoint's metadata (§2.1). Registration and the
// heartbeat's endpoints declaration (§8.3) both use it, since the declaration
// must copy the registration verbatim.
func (s *Server) promptMetadata() map[string]string {
	attachmentsOk := "false"
	if s.cfg.attachmentsOk {
		attachmentsOk = attachmentsOkTrue
	}

	return map[string]string{
		"max_payload":    s.cfg.maxPayload,
		"attachments_ok": attachmentsOk,
	}
}

// register adds the micro service and its endpoints and starts the heartbeat
// publisher. Callers hold s.mu. On error nothing is left registered.
func (s *Server) register(serveCtx, promptCtx context.Context) (natsio.Service, error) {
	metadata := map[string]string{
		metaAgent:           s.cfg.agentID,
		metaOwner:           s.owner,
		metaProtocolVersion: protocolVersion,
	}
	if s.cfg.session != "" {
		metadata[metaSession] = s.cfg.session
	}

	svc, err := natsio.AddService(s.nc, natsio.Config{
		Name:        svcNameAgents,
		Version:     s.cfg.version,
		Description: fmt.Sprintf("%s/%s — %s", s.cfg.agentID, s.owner, s.name),
		Metadata:    metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("nats: register micro service: %w", err)
	}

	statusSubject := fmt.Sprintf("agents.status.%s.%s.%s", s.cfg.agentID, s.owner, s.name)
	hbSubject := fmt.Sprintf("agents.hb.%s.%s.%s", s.cfg.agentID, s.owner, s.name)

	// Admit prompts before the endpoint exists, not after: a request delivered
	// between the two would otherwise find the gate shut and be refused by a
	// server that is perfectly healthy.
	s.gate.unlock()

	if addErr := svc.AddEndpoint(endpointPrompt,
		natsio.ContextHandler(promptCtx, s.handlePrompt),
		natsio.WithEndpointSubject(s.promptSubject()),
		natsio.WithEndpointQueueGroup(svcNameAgents),
		natsio.WithEndpointMetadata(s.promptMetadata()),
	); addErr != nil {
		_ = svc.Stop()
		return nil, fmt.Errorf("nats: register prompt endpoint: %w", addErr)
	}

	if addErr := svc.AddEndpoint("status",
		natsio.ContextHandler(serveCtx, s.handleStatus),
		natsio.WithEndpointSubject(statusSubject),
		natsio.WithEndpointQueueGroup(svcNameAgents),
	); addErr != nil {
		_ = svc.Stop()
		return nil, fmt.Errorf("nats: register status endpoint: %w", addErr)
	}

	instanceID := svc.Info().ID

	s.hbWG.Go(func() { s.startHeartbeats(serveCtx, hbSubject, instanceID) })

	return svc, nil
}

// handlePrompt dispatches each prompt request into a goroutine so that the
// NATS subscription callback returns immediately and the subscription remains
// responsive to further requests (§3.4).
//
// A request that arrives once a drain has begun is refused rather than served.
// Unsubscribing the endpoint is asynchronous — the micro service drains its
// subscriptions — so a request buffered before that can still land here, and
// serving it would mean admitting work into a shutdown nobody is waiting on.
//
// The refusal is a 500 carrying error code "server_draining". §9.2 says agents
// MUST use codes from its table, which has no 503; 500 is the one that keeps the
// meaning callers need, a server-side fault worth retrying against a replica
// that is not going away, and the body code says why.
func (s *Server) handlePrompt(ctx context.Context, req natsio.Request) {
	if !s.gate.enter() {
		sendError(req, "500", errCodeServerDraining, drainingMessage)

		return
	}

	go func() {
		defer s.gate.leave()

		s.processPrompt(ctx, req)
	}()
}

// processPrompt decodes the envelope, invokes the agent, streams the result,
// and always terminates the response stream with the zero-byte terminator (§6.5).
func (s *Server) processPrompt(ctx context.Context, req natsio.Request) {
	badRequest := func(errCode, message string) { sendError(req, "400", errCode, message) }

	env, err := decodeEnvelope(req.Data())
	if err != nil {
		badRequest("malformed_envelope", err.Error())
		return
	}

	if !s.cfg.attachmentsOk && len(env.Attachments) > 0 {
		badRequest("attachments_not_allowed", "this agent does not accept attachments")
		return
	}

	parts, err := envelopeToContentParts(env)
	if err != nil {
		badRequest("malformed_envelope", err.Error())
		return
	}

	request, err := requestFromEnvelope(env, natsclient.Header(req.Headers()))
	if err != nil {
		badRequest("malformed_envelope", err.Error())
		return
	}

	// The handler — and, through an agent, its tools — can read the request it
	// is serving with RequestFrom.
	ctx = withRequest(ctx, request)

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
		sendCodedError(req, s.errorFor(ctx, runErr))
		return
	}

	s.warnDroppedHandoffs(ctx, result)
	s.sendResponse(req, result.TextContent())
}

// warnDroppedHandoffs logs the handoffs in result, which the response cannot
// carry: the protocol has no field for them, and the caller — another process,
// perhaps another SDK — could not run a phero agent anyway. Without the
// warning, an agent that hands off locally would silently stop doing so once
// served over NATS. Only the text is sent; Summary is dropped too, on every
// call, so it is documented rather than logged.
func (s *Server) warnDroppedHandoffs(ctx context.Context, result *agent.Result) {
	if result == nil || len(result.HandoffAgents) == 0 {
		return
	}

	names := make([]string, 0, len(result.HandoffAgents))
	for _, a := range result.HandoffAgents {
		if a != nil {
			names = append(names, a.Name())
		}
	}

	s.logger().WarnContext(ctx, "nats: handoffs dropped: the protocol cannot carry them",
		slog.String("owner", s.owner),
		slog.String("name", s.name),
		slog.Any("handoffs", names),
	)
}

// logger returns the configured logger, or slog.Default().
func (s *Server) logger() *slog.Logger {
	if s.cfg.logger != nil {
		return s.cfg.logger
	}

	return slog.Default()
}

// sendResponse sends text as one or more response chunks (§6.3), each small
// enough to publish on this connection, then the terminator (§6.5).
//
// A failed publish is reported, not ignored: ignoring it would still send the
// terminator, and the caller would read a lost answer as an empty one. The
// error follows whatever content already went out, which §9.3 allows, and the
// caller discards that partial text when it sees the error.
func (s *Server) sendResponse(req natsio.Request, text string) {
	chunks, ok := encodeResponseChunks(text, s.maxPublishBytes())
	if !ok {
		sendError(req, "500", errCodeResponseTooLarge, "response does not fit in this connection's max_payload")
		return
	}

	for _, chunk := range chunks {
		if err := req.Respond(chunk); err != nil {
			errCode := errCodeResponseFailed
			if errors.Is(err, natsclient.ErrMaxPayload) {
				errCode = errCodeResponseTooLarge
			}

			sendError(req, "500", errCode, "send response: "+err.Error())

			return
		}
	}

	_ = req.Respond(nil) // terminator (§6.5)
}

// maxPublishBytes is the largest message this server can publish: the
// connection's max_payload, or NATS's default when the connection does not
// report one (it is not connected, or there is none, as in tests).
func (s *Server) maxPublishBytes() int {
	if s.nc != nil {
		if n := s.nc.MaxPayload(); n > 0 {
			return int(n)
		}
	}

	return natsDefaultMaxPayload
}

// sendError sends a service error (§9.1) followed by the terminator (§9.3).
func sendError(req natsio.Request, code, errCode, message string) {
	sendErrorBody(req, code, headerSafe(message), encodeErrorBody(errCode, message, 0))
}

// sendErrorBody sends a service error with a prepared body (§9.1), followed by
// the terminator (§9.3).
func sendErrorBody(req natsio.Request, code, description string, body []byte) {
	_ = req.Error(code, description, body)
	_ = req.Respond(nil) // terminator (§9.3)
}

// handleStatus replies with a heartbeat-shaped JSON payload (§8.7).
// The request body is ignored per the spec.
func (s *Server) handleStatus(_ context.Context, req natsio.Request) {
	instanceID := ""

	s.mu.Lock()
	svc := s.svc
	s.mu.Unlock()

	if svc != nil {
		instanceID = svc.Info().ID
	}

	_ = req.Respond(encodeHeartbeat(s.heartbeat(instanceID)))
}
