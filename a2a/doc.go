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
package a2a
