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
// Wire format is defined by the NATS Agent Protocol spec (core-protocol.md).
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
//	srv.Start(ctx) // blocks until ctx is cancelled
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
