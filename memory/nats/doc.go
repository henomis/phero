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
// Several processes can share one session. Save writes with a compare-and-swap
// on the key's revision: a writer that lost the race reloads the history and
// tries again, so no messages are overwritten. A Save that keeps losing gives
// up with [ErrConcurrentUpdate] and stores nothing.
//
// A session is stored as one value, so it must fit in one NATS message: the
// server's max_payload (1MB by default), or the bucket's MaxValueSize if that
// is smaller. Images count base64-encoded. Past the limit Save returns
// [ErrSessionTooLarge], and so will every later Save to that session. To stay
// under it, enable [WithSummarization] (which also drops the images of the
// messages it summarizes), start a new session, raise max_payload, or use
// memory/psql, which stores each message as its own row.
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
//		natsmemory "github.com/henomis/phero/v2/memory/nats"
//	)
//
//	nc, _ := nats.Connect(os.Getenv("NATS_URL"))
//	js, _ := nc.JetStream()
//	kv, _ := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: "phero_memory"})
//
//	mem, _ := natsmemory.New(kv, "session-123")
//	_ = mem.Save(context.Background(), messages)
package natsmemory
