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

// Package a2a exposes phero agents as A2A servers and lets agents call
// remote A2A endpoints as tools.
//
// # Server side
//
// Wrap a [agent.Agent] with [New], providing the public base URL and any
// [ServerOption] values needed:
//
//	srv, err := a2a.New(myAgent, "https://agent.example.com",
//	    a2a.WithVersion("2.0"),
//	    a2a.WithStreaming(),
//	    a2a.WithRESTTransport(),
//	    a2a.WithProvider("Acme Corp", "https://acme.example.com"),
//	    a2a.WithSkills(sdka2a.AgentSkill{ID: "summarize", Name: "Summarize"}),
//	)
//
// Register handlers on an [http.ServeMux] with one call:
//
//	srv.Mount(mux)
//	// or individually:
//	mux.Handle("/.well-known/agent-card.json", srv.AgentCardHandler())
//	mux.Handle("/", srv.JSONRPCHandler())
//	// when WithRESTTransport is used — must strip the /rest prefix:
//	mux.Handle("/rest/", http.StripPrefix("/rest", srv.RESTHandler()))
//
// Key server options:
//   - [WithVersion] — agent version in the AgentCard
//   - [WithSkills] — additional skill entries in the AgentCard
//   - [WithInputModes] / [WithOutputModes] — supported MIME types
//   - [WithProvider] — provider organisation and URL
//   - [WithIconURL] / [WithDocURL] — card metadata URLs
//   - [WithRESTTransport] — enable HTTP+JSON/SSE transport
//   - [WithStreaming] — advertise streaming capability in the AgentCard
//   - [WithPushNotifications] — enable push notification endpoints
//   - [WithTaskStore] — persistent task state backend
//   - [WithCallInterceptors] — request/response middleware (auth, logging, …)
//   - [WithExecutorContextInterceptors] — enrich the ExecutorContext per-call
//   - [WithConcurrencyLimit] — cap concurrent agent invocations
//   - [WithLogger] — custom structured logger
//   - [WithExtendedCard] — authenticated extended AgentCard
//   - [WithClusterMode] — distributed execution via work queue
//   - [WithHandlerOption] — escape hatch for raw handler options
//
// Cancellation: when a client cancels a task the running [agent.Agent.Run] call
// is interrupted via context cancellation, so long-running agents stop promptly.
//
// Multimodal: incoming A2A text, URL, and raw-bytes parts are translated to the
// corresponding phero [llm.ContentPart] types and vice-versa. Structured data
// parts are JSON-encoded and passed as text.
//
// # Client side
//
// Use [NewClient] to resolve a remote agent's AgentCard and then call
// [Client.AsTool] to obtain an [llm.Tool] that a phero agent can invoke:
//
//	client, err := a2a.NewClient(ctx, "http://remote-agent:8080",
//	    a2a.WithAcceptedOutputModes("text/plain"),
//	    a2a.WithPreferredTransports(sdka2a.TransportProtocolHTTPJSON),
//	)
//	tool, err := client.AsTool()
//	myAgent.AddTool(tool)
//
// The tool handler transparently handles both synchronous (inline Message) and
// asynchronous (Task-based) responses. For async tasks it subscribes to the
// event stream or falls back to polling GetTask at the configured interval
// (default 500 ms) until the task reaches a terminal state.
//
// Key client options:
//   - [WithResolver] — override the default AgentCard resolver
//   - [WithPushConfig] — default push notification config for tasks
//   - [WithAcceptedOutputModes] — MIME types the client can consume
//   - [WithPreferredTransports] — ordered transport protocol preferences
//   - [WithClientInterceptors] — inject auth headers or tracing per call
//   - [WithPollingInterval] — tune the GetTask polling fallback interval
//
// # Operational responsibilities
//
// The package only returns plain [http.Handler] values. TLS termination,
// authentication, authorisation, rate limiting, and request-validation
// middleware are entirely the caller's responsibility. The baseURL passed to
// [New] is published verbatim in the AgentCard, so it must be the externally
// reachable endpoint at which the handlers are mounted.
package a2a
