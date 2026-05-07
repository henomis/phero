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

// Package natsmemory implements the memory.Memory interface using NATS JetStream Key-Value.
//
// Conversation messages are stored as a JSON-encoded []llm.Message under a
// per-session key inside a JetStream KV bucket. Because NATS JetStream
// persists bucket data to disk, memory survives process restarts.
//
// Requirements:
//   - A NATS server with JetStream enabled (start with: nats -js).
//   - A pre-created nats.KeyValue bucket, injected at construction time.
//
// Basic usage:
//
//	import (
//		"context"
//		"os"
//
//		"github.com/nats-io/nats.go"
//
//		natsmemory "github.com/henomis/phero/memory/nats"
//	)
//
//	nc, _ := nats.Connect(os.Getenv("NATS_URL"))
//	js, _ := nc.JetStream()
//	kv, _ := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: "phero_memory"})
//
//	mem, _ := natsmemory.New(kv, "session-123")
//	_ = mem.Save(context.Background(), messages)
package natsmemory
