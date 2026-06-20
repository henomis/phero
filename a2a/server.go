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
	"log/slog"
	"net/http"
	"net/url"
	"sync"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/limiter"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	"github.com/henomis/phero/agent"
)

// restPathPrefix is the URL path segment used for the HTTP+JSON/SSE transport.
// It is appended to baseURL in AgentCard() and stripped before routing in
// Mount(), keeping both sites consistent via a single definition.
const (
	restPathPrefix = "/rest"
	mimeTextPlain  = "text/plain"
)

// ServerOption configures a [Server].
type ServerOption func(*serverConfig)

type serverConfig struct {
	version      string
	skills       []sdka2a.AgentSkill
	inputModes   []string
	outputModes  []string
	providerOrg  string
	providerURL  string
	iconURL      string
	docURL       string
	enableREST   bool
	capabilities sdka2a.AgentCapabilities
	handlerOpts  []a2asrv.RequestHandlerOption
}

// WithVersion sets the agent version advertised in the AgentCard. Defaults to "1.0".
func WithVersion(v string) ServerOption {
	return func(cfg *serverConfig) { cfg.version = v }
}

// WithSkills appends additional [sdka2a.AgentSkill] entries to the AgentCard.
// The agent's own name and description are always included as the first skill.
func WithSkills(skills ...sdka2a.AgentSkill) ServerOption {
	return func(cfg *serverConfig) { cfg.skills = append(cfg.skills, skills...) }
}

// WithInputModes overrides the default input MIME types in the AgentCard.
// Defaults to [mimeTextPlain].
func WithInputModes(modes ...string) ServerOption {
	return func(cfg *serverConfig) { cfg.inputModes = modes }
}

// WithOutputModes overrides the default output MIME types in the AgentCard.
// Defaults to [mimeTextPlain].
func WithOutputModes(modes ...string) ServerOption {
	return func(cfg *serverConfig) { cfg.outputModes = modes }
}

// WithProvider sets the provider organisation and URL in the AgentCard.
func WithProvider(org, providerURL string) ServerOption {
	return func(cfg *serverConfig) { cfg.providerOrg = org; cfg.providerURL = providerURL }
}

// WithIconURL sets the agent icon URL in the AgentCard.
func WithIconURL(iconURL string) ServerOption {
	return func(cfg *serverConfig) { cfg.iconURL = iconURL }
}

// WithDocURL sets the agent documentation URL in the AgentCard.
func WithDocURL(docURL string) ServerOption {
	return func(cfg *serverConfig) { cfg.docURL = docURL }
}

// WithRESTTransport enables the HTTP+JSON/SSE transport in addition to JSON-RPC.
// The AgentCard will advertise a second interface at <baseURL>/rest using the
// HTTP+JSON protocol, and [Server.RESTHandler] will return a non-nil handler.
func WithRESTTransport() ServerOption {
	return func(cfg *serverConfig) { cfg.enableREST = true }
}

// WithStreaming declares that the agent supports streaming responses.
// This sets Capabilities.Streaming in the AgentCard so that clients know they
// can use the streaming SendMessage variant. The underlying handler already
// supports streaming; this option merely makes the capability visible.
func WithStreaming() ServerOption {
	return func(cfg *serverConfig) { cfg.capabilities.Streaming = true }
}

// WithPushNotifications enables push notification support.
// store is used to persist push configs per task; sender delivers the push events.
// The AgentCard will advertise PushNotifications in Capabilities.
func WithPushNotifications(store push.ConfigStore, sender push.Sender) ServerOption {
	return func(cfg *serverConfig) {
		cfg.capabilities.PushNotifications = true
		cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithPushNotifications(store, sender))
	}
}

// WithTaskStore replaces the default in-memory task store with a custom implementation.
// Use this to persist task state across restarts or share it in a cluster.
func WithTaskStore(store taskstore.Store) ServerOption {
	return func(cfg *serverConfig) {
		cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithTaskStore(store))
	}
}

// WithCallInterceptors registers request/response interceptors on the handler.
// Interceptors run before and after every protocol method, enabling cross-cutting
// concerns such as authentication, logging, and rate limiting.
func WithCallInterceptors(interceptors ...a2asrv.CallInterceptor) ServerOption {
	return func(cfg *serverConfig) {
		cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithCallInterceptors(interceptors...))
	}
}

// WithExecutorContextInterceptors registers one or more [a2asrv.ExecutorContextInterceptor]
// values that can enrich the ExecutorContext before each agent invocation.
func WithExecutorContextInterceptors(interceptors ...a2asrv.ExecutorContextInterceptor) ServerOption {
	return func(cfg *serverConfig) {
		for _, i := range interceptors {
			cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithExecutorContextInterceptor(i))
		}
	}
}

// WithConcurrencyLimit sets the maximum number of concurrent agent executions.
func WithConcurrencyLimit(config limiter.ConcurrencyConfig) ServerOption {
	return func(cfg *serverConfig) {
		cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithConcurrencyConfig(config))
	}
}

// WithLogger sets a custom structured logger for the handler.
func WithLogger(logger *slog.Logger) ServerOption {
	return func(cfg *serverConfig) {
		cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithLogger(logger))
	}
}

// WithExtendedCard configures a producer for the authenticated extended AgentCard.
// The AgentCard will advertise ExtendedAgentCard in Capabilities.
func WithExtendedCard(producer a2asrv.ExtendedAgentCardProducer) ServerOption {
	return func(cfg *serverConfig) {
		cfg.capabilities.ExtendedAgentCard = true
		cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithExtendedAgentCardProducer(producer))
	}
}

// WithClusterMode enables distributed execution using the provided work queue,
// event queue manager, and shared task store.
func WithClusterMode(config a2asrv.ClusterConfig) ServerOption {
	return func(cfg *serverConfig) {
		cfg.handlerOpts = append(cfg.handlerOpts, a2asrv.WithClusterMode(config))
	}
}

// WithHandlerOption passes a raw [a2asrv.RequestHandlerOption] to the underlying
// handler. Use this for advanced configuration not covered by the higher-level options.
func WithHandlerOption(opt a2asrv.RequestHandlerOption) ServerOption {
	return func(cfg *serverConfig) {
		cfg.handlerOpts = append(cfg.handlerOpts, opt)
	}
}

// Server wraps a phero [agent.Agent] and exposes it as an A2A-compliant HTTP
// handler set supporting JSON-RPC and optionally HTTP+JSON/SSE transports.
type Server struct {
	agent   *agent.Agent
	baseURL string
	cfg     *serverConfig

	// handler is built once and shared by all transport handlers so they
	// use the same in-memory task store and event queues.
	handler     a2asrv.RequestHandler
	handlerOnce sync.Once
}

// New creates a new Server for the provided agent and public base URL.
//
// baseURL must be an absolute URL with a scheme and host
// (e.g. "https://agent.example.com"). It is published verbatim in the AgentCard
// so it must be the externally reachable root at which the handlers are mounted.
func New(a *agent.Agent, baseURL string, opts ...ServerOption) (*Server, error) {
	if a == nil {
		return nil, ErrAgentRequired
	}

	if baseURL == "" {
		return nil, ErrBaseURLRequired
	}

	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, ErrInvalidBaseURL
	}

	cfg := &serverConfig{
		version:     "1.0",
		inputModes:  []string{mimeTextPlain},
		outputModes: []string{mimeTextPlain},
	}

	for _, o := range opts {
		o(cfg)
	}

	return &Server{agent: a, baseURL: baseURL, cfg: cfg}, nil
}

// AgentCard returns the A2A AgentCard derived from the wrapped agent and server configuration.
//
// The card always includes a JSON-RPC interface at baseURL. If [WithRESTTransport]
// was used, a second HTTP+JSON interface at <baseURL>/rest is added. Capabilities
// and provider metadata reflect whatever options were passed to [New].
func (s *Server) AgentCard() *sdka2a.AgentCard {
	interfaces := []*sdka2a.AgentInterface{
		sdka2a.NewAgentInterface(s.baseURL, sdka2a.TransportProtocolJSONRPC),
	}
	if s.cfg.enableREST {
		interfaces = append(interfaces,
			sdka2a.NewAgentInterface(s.baseURL+restPathPrefix, sdka2a.TransportProtocolHTTPJSON),
		)
	}

	card := &sdka2a.AgentCard{
		Name:                s.agent.Name(),
		Description:         s.agent.Description(),
		Version:             s.cfg.version,
		IconURL:             s.cfg.iconURL,
		DocumentationURL:    s.cfg.docURL,
		SupportedInterfaces: interfaces,
		DefaultInputModes:   s.cfg.inputModes,
		DefaultOutputModes:  s.cfg.outputModes,
		Skills:              s.buildSkills(),
		Capabilities:        s.cfg.capabilities,
	}

	if s.cfg.providerOrg != "" {
		card.Provider = &sdka2a.AgentProvider{
			Org: s.cfg.providerOrg,
			URL: s.cfg.providerURL,
		}
	}

	return card
}

// AgentCardHandler returns an [http.Handler] for the well-known AgentCard endpoint
// (/.well-known/agent-card.json).
func (s *Server) AgentCardHandler() http.Handler {
	return a2asrv.NewStaticAgentCardHandler(s.AgentCard())
}

// JSONRPCHandler returns an [http.Handler] for the A2A JSON-RPC endpoint.
func (s *Server) JSONRPCHandler() http.Handler {
	return a2asrv.NewJSONRPCHandler(s.getHandler())
}

// RESTHandler returns an [http.Handler] for the A2A HTTP+JSON/SSE endpoint.
// Returns nil if [WithRESTTransport] was not used.
//
// The returned handler expects requests rooted at / — callers that mount it
// under a prefix (e.g. /rest/) must strip that prefix first, for example
// with [http.StripPrefix].
func (s *Server) RESTHandler() http.Handler {
	if !s.cfg.enableREST {
		return nil
	}

	return a2asrv.NewRESTHandler(s.getHandler())
}

// Mount registers all active handlers on mux using canonical paths:
//
//	/.well-known/agent-card.json → AgentCardHandler
//	/                           → JSONRPCHandler
//	/rest/                      → RESTHandler (only when WithRESTTransport was used)
//
// The REST handler is mounted with the /rest prefix stripped so that the
// internal routing table of the A2A REST handler (which uses paths like
// /message:send, /tasks, etc.) receives paths relative to the mount point.
func (s *Server) Mount(mux *http.ServeMux) {
	mux.Handle("/.well-known/agent-card.json", s.AgentCardHandler())
	mux.Handle("/", s.JSONRPCHandler())

	if s.cfg.enableREST {
		mux.Handle(restPathPrefix+"/", http.StripPrefix(restPathPrefix, s.RESTHandler()))
	}
}

// getHandler returns the shared RequestHandler, building it on first call.
// Both JSONRPCHandler and RESTHandler share the same instance so they use
// the same in-memory task store, event queues, and concurrency state.
func (s *Server) getHandler() a2asrv.RequestHandler {
	s.handlerOnce.Do(func() {
		executor := &agentExecutor{agent: s.agent}
		s.handler = a2asrv.NewHandler(executor, s.cfg.handlerOpts...)
	})

	return s.handler
}

// buildSkills returns the agent's primary skill followed by any extra skills
// registered via [WithSkills].
func (s *Server) buildSkills() []sdka2a.AgentSkill {
	primary := sdka2a.AgentSkill{
		ID:          s.agent.Name(),
		Name:        s.agent.Name(),
		Description: s.agent.Description(),
		Tags:        []string{},
	}
	skills := make([]sdka2a.AgentSkill, 0, 1+len(s.cfg.skills))
	skills = append(skills, primary)
	skills = append(skills, s.cfg.skills...)

	return skills
}
