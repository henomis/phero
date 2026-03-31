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
// Server side: wrap a [agent.Agent] with [New], providing the required public
// base URL. The resulting [Server] exposes [Server.AgentCardHandler] and
// [Server.JSONRPCHandler] so the caller can mount them at caller-chosen
// endpoints in its own HTTP server, router, and middleware stack.
//
// Client side: use [NewClient] to resolve a remote agent's card and then call
// [Client.AsTool] to obtain an [llm.Tool] that a phero agent can invoke.
//
// # Current limitations
//
// This package is a minimal, text-focused A2A bridge and does not implement
// the full A2A protocol surface. Callers should be aware of the following
// constraints.
//
// Protocol coverage: only a single JSON-RPC interface is advertised. The
// generated [Server.AgentCard] always declares text/plain as the sole input
// and output mode and exposes exactly one skill derived from the wrapped agent.
// Multiple transports, richer capability metadata, and multi-skill cards are
// not supported.
//
// Message handling: the server reads only text parts from an incoming A2A
// message; non-text parts, file attachments, and richer message metadata are
// silently ignored. If no text part is present the agent receives an empty
// input string. Multiple text parts are concatenated without a separator.
// The client adapter likewise sends a single plain-text user message and
// returns an error if the remote response contains no text content.
//
// Task-based flows: [Client.AsTool] makes one [SendMessage] call and reads
// the response inline. It succeeds only when the remote server returns the
// final text immediately, either in the response Message or in the Task status
// payload. The package does not expose separate task-polling, resumption, or
// streaming APIs; long-running asynchronous tasks are therefore not supported
// by this adapter.
//
// Cancellation: [Server.Cancel] reports a protocol-level cancelled status to
// the caller but does not interrupt an in-flight phero agent. Agents always
// run to completion or until they return an error.
//
// Operational responsibilities: the package only returns plain [http.Handler]
// values. TLS termination, authentication, authorization, rate limiting, and
// any request-validation middleware are entirely the caller's responsibility.
// The baseURL passed to [New] is published verbatim as the JSON-RPC interface
// URL inside the Agent Card, so it must be the externally reachable endpoint
// at which [Server.JSONRPCHandler] is mounted.
package a2a
