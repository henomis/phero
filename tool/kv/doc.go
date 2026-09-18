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

// Package kv provides Phero tools that let an agent read and write a NATS
// JetStream key-value bucket — a scratchpad that outlives a single run and can
// be shared between agents.
//
// It is distinct from memory/nats, which stores an agent's conversation over
// the same primitive without the agent choosing what goes in. Here the agent
// decides: what to note, under which key, and when to look it up.
//
//	store, _ := kv.Open(nc, "agent-notes")
//	tools, _ := store.Tools()
//	for _, t := range tools {
//		_ = myAgent.AddTool(t)
//	}
//
// # Scope
//
// The tools expose the whole bucket. An agent can read any key in it, including
// keys written by another agent or another session, and overwrite them. Where
// that matters, give each trust boundary its own bucket rather than relying on
// key naming — the model chooses the keys.
package kv
