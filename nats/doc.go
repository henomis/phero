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

// Package nats implements the NATS Agent Protocol v0.3 for Phero agents.
//
// It exposes two top-level types:
//
//   - [Server] registers any [agent.Agent] as a NATS micro service, handling
//     discovery (via $SRV.PING/INFO), streaming prompt responses, periodic
//     heartbeats, and on-demand status replies.
//
//   - [Client] discovers compliant agents on the same NATS server and sends
//     them prompts. [Client.AsTool] wraps a remote agent as an [llm.Tool] that
//     any local Phero agent can call.
//
// Wire format is defined by the NATS Agent Protocol spec
// (https://github.com/synadia-ai/synadia-agent-sdk-docs/blob/main/core-protocol.md).
// This implementation is wire-compatible with the TypeScript and Python SDKs
// in the synadia-agents repository.
//
// Quick start — server:
//
//	nc, _ := nats.Connect(nats.DefaultURL)
//	defer nc.Drain()
//
//	a, _ := agent.New(llmClient, "my-agent", "A helpful assistant")
//
//	srv, _ := natsagent.New(nc, a, "alice", "my-agent")
//	srv.Start(ctx) // serves until ctx is cancelled, then drains
//
// Cancelling that context means "shut down", not "abandon what you are doing":
// the prompt endpoint is unsubscribed so further prompts go to another replica,
// and the handlers already running keep a context of their own and finish. See
// [Server.Drain] and [WithDrainTimeout] for the budget. Drain is also how you
// trigger the same shutdown from elsewhere, on a context of your own.
//
// Start blocks for the server's whole life; [Server.Ready] is closed once the
// broker has the registration and the first heartbeat, so the agent can be
// discovered and prompted. It is never closed if Start fails or the server
// drains first, so wait on it together with Start's result.
//
// A prompt must fit in one NATS message, so it is limited by the agent's
// advertised max_payload. An answer is not: one larger than the server
// connection's max_payload is sent as several response chunks, which
// [Stream.Text] joins back together. An answer that still cannot be published
// is reported as a 500 service error ("response_too_large" or
// "response_failed"), never as an empty answer.
//
// Errors from a call can be sorted with [Permanent]: a rate limit (429) is
// worth retrying, and a [ServiceError] says how long to wait in RetryAfter.
// The client cannot answer an agent that asks a question mid-stream:
// [Stream.Text] then returns a [QueryError] carrying the question.
//
// A handler chooses the error its caller receives by returning a
// [CodedError]; errors from the LLM are mapped from [llm.ProviderError], and
// [WithErrorMapper] overrides both.
//
// [Client.Send] sends a [Request] with attachments and extra headers; on the
// server, the handler (and an agent's tools) read it with [RequestFrom].
//
// Only the text of an agent's Result crosses the wire. Handoffs are dropped —
// the caller could not run them, and receives only the handoff tool's
// acknowledgement — and the server logs a warning (see [WithLogger]). Token
// usage (Result.Summary) is dropped too; collect it on the server's side.
//
// Quick start — client:
//
//	nc, _ := nats.Connect(nats.DefaultURL)
//	defer nc.Drain()
//
//	c := natsagent.NewClient(nc)
//	agents, _ := c.Discover(ctx)
//	stream, _ := c.Prompt(ctx, agents[0], "Hello!")
//	text, _ := stream.Text(ctx)
//	fmt.Println(text)
package nats
