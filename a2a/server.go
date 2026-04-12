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
	"iter"
	"net/http"
	"net/url"
	"strings"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// Server wraps a phero [agent.Agent] and exposes it as an A2A-compliant HTTP
// handler set.
type Server struct {
	agent   *agent.Agent
	baseURL string
}

// New creates a new Server for the provided agent and public base URL.
//
// baseURL should be the externally reachable origin of the agent, such as
// "https://agent.example.com".
func New(a *agent.Agent, baseURL string) (*Server, error) {
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

	s := &Server{
		agent:   a,
		baseURL: baseURL,
	}

	return s, nil
}

// AgentCard returns the A2A AgentCard derived from the wrapped agent.
func (s *Server) AgentCard() *sdka2a.AgentCard {
	url := s.baseURL

	return &sdka2a.AgentCard{
		Name:        s.agent.Name(),
		Description: s.agent.Description(),
		Version:     "1.0",
		SupportedInterfaces: []*sdka2a.AgentInterface{
			sdka2a.NewAgentInterface(url, sdka2a.TransportProtocolJSONRPC),
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills: []sdka2a.AgentSkill{
			{
				ID:          s.agent.Name(),
				Name:        s.agent.Name(),
				Description: s.agent.Description(),
				Tags:        []string{},
			},
		},
	}
}

// AgentCardHandler returns an [http.Handler] for the well-known AgentCard
// endpoint.
func (s *Server) AgentCardHandler() http.Handler {
	return a2asrv.NewStaticAgentCardHandler(s.AgentCard())
}

// JSONRPCHandler returns an [http.Handler] for the A2A JSON-RPC endpoint.
func (s *Server) JSONRPCHandler() http.Handler {
	executor := &agentExecutor{agent: s.agent}

	return a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(executor))
}

// agentExecutor bridges the A2A AgentExecutor interface to a phero agent.
type agentExecutor struct {
	agent *agent.Agent
}

// Execute implements [a2asrv.AgentExecutor].
//
// It extracts the first text part from the incoming message, runs the phero
// agent, and emits A2A events for submitted → working → completed (or failed).
func (e *agentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[sdka2a.Event, error] {
	return func(yield func(sdka2a.Event, error) bool) {
		// Announce the task as submitted when it is new.
		if execCtx.StoredTask == nil {
			if !yield(sdka2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}

		// Signal that the agent is working.
		if !yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateWorking, nil), nil) {
			return
		}

		// Extract the user input from the incoming message.
		input := extractText(execCtx.Message)

		// Run the phero agent.
		result, err := e.agent.Run(ctx, llm.Text(input))
		if err != nil {
			errMsg := sdka2a.NewMessageForTask(sdka2a.MessageRoleAgent, execCtx, sdka2a.NewTextPart(err.Error()))
			yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateFailed, errMsg), nil)
			return
		}

		// Emit the completed event with the agent's response.
		responseMsg := sdka2a.NewMessageForTask(sdka2a.MessageRoleAgent, execCtx, sdka2a.NewTextPart(result.TextContent()))
		yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateCompleted, responseMsg), nil)
	}
}

// Cancel implements [a2asrv.AgentExecutor].
//
// Phero agents do not support mid-execution cancellation; this emits a
// cancelled status immediately.
func (e *agentExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[sdka2a.Event, error] {
	return func(yield func(sdka2a.Event, error) bool) {
		yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateCanceled, nil), nil)
	}
}

// extractText returns the concatenated text of all text parts in msg.
// Returns an empty string if msg is nil or contains no text parts.
func extractText(msg *sdka2a.Message) string {
	if msg == nil {
		return ""
	}

	var builder strings.Builder

	for _, part := range msg.Parts {
		if t := part.Text(); t != "" {
			builder.WriteString(t)
		}
	}

	return builder.String()
}
