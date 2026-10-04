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

//go:build e2e

package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	natsio "github.com/nats-io/nats.go"

	"github.com/henomis/phero/v2/llm"
	natsmemory "github.com/henomis/phero/v2/memory/nats"
	"github.com/henomis/phero/v2/tool/kv"
)

// TestKVToolsReadWrite drives the tools the way an agent would: write a note,
// read it back, and read a key that was never written.
func TestKVToolsReadWrite(t *testing.T) {
	nc := requireNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := kv.Open(nc, "phero-e2e-kvtool")
	if err != nil {
		t.Fatalf("kv.Open: %v", err)
	}

	t.Cleanup(func() {
		js, jsErr := nc.JetStream()
		if jsErr == nil {
			_ = js.DeleteKeyValue("phero-e2e-kvtool")
		}
	})

	tools, err := store.Tools()
	if err != nil {
		t.Fatalf("Tools: %v", err)
	}

	if len(tools) != 2 {
		t.Fatalf("got %d tools, want 2 (read and write)", len(tools))
	}

	read, write := tools[0], tools[1]

	if _, err = write.Handle(ctx, `{"key":"note","value":"remember this"}`); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := read.Handle(ctx, `{"key":"note"}`)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if text, _ := got.(string); text != "remember this" {
		t.Errorf("read returned %q, want %q", text, "remember this")
	}

	// A missing key is an ordinary answer, not a tool failure: an agent checking
	// its own notes should be told "nothing yet", not handed an error to reason
	// about.
	missing, err := read.Handle(ctx, `{"key":"never-written"}`)
	if err != nil {
		t.Fatalf("read of a missing key returned an error: %v", err)
	}

	if text, _ := missing.(string); text != "" {
		t.Errorf("read of a missing key returned %q, want an empty string", text)
	}
}

// Open must be safe to call concurrently against a bucket that does not exist
// yet — two replicas starting at once is the race that makes the naive
// create-then-fail shape wrong.
func TestKVOpenIsSafeForConcurrentCreation(t *testing.T) {
	nc := requireNATS(t)

	t.Cleanup(func() {
		js, jsErr := nc.JetStream()
		if jsErr == nil {
			_ = js.DeleteKeyValue("phero-e2e-kvrace")
		}
	})

	const racers = 8

	var wg sync.WaitGroup

	errs := make([]error, racers)

	for i := range racers {
		wg.Go(func() {
			_, errs[i] = kv.Open(nc, "phero-e2e-kvrace")
		})
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("racer %d: %v", i, err)
		}
	}
}

// memory/nats.Open is the same create-or-bind, for the memory backend.
func TestNATSMemoryOpenCreatesAndBinds(t *testing.T) {
	nc := requireNATS(t)

	t.Cleanup(func() {
		js, jsErr := nc.JetStream()
		if jsErr == nil {
			_ = js.DeleteKeyValue("phero-e2e-memopen")
		}
	})

	// First call creates the bucket, second binds the existing one.
	for i := range 2 {
		mem, err := natsmemory.Open(nc, "phero-e2e-memopen", "session-1")
		if err != nil {
			t.Fatalf("Open (call %d): %v", i, err)
		}

		if mem == nil {
			t.Fatalf("Open (call %d) returned a nil memory", i)
		}
	}
}

// TestNATSMemoryConcurrentSavesKeepEveryMessage is issue #2: two processes
// sharing a session save at the same time. Each Memory stands for a process,
// with its own mutex, so only the revision check keeps their writes apart.
func TestNATSMemoryConcurrentSavesKeepEveryMessage(t *testing.T) {
	nc := requireNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const (
		bucket    = "phero-e2e-memrace"
		processes = 2
		writers   = 4 // goroutines per process
		saves     = 10
	)

	t.Cleanup(func() {
		js, jsErr := nc.JetStream()
		if jsErr == nil {
			_ = js.DeleteKeyValue(bucket)
		}
	})

	session := "race-" + uuid.NewString()

	mems := make([]*natsmemory.Memory, processes)
	for i := range mems {
		mem, err := natsmemory.Open(nc, bucket, session)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		mems[i] = mem
	}

	var wg sync.WaitGroup

	errs := make(chan error, processes*writers*saves)

	for p, mem := range mems {
		for w := range writers {
			wg.Go(func() {
				for n := range saves {
					msg := llm.UserMessage(llm.Text(fmt.Sprintf("p%d-w%d-%d", p, w, n)))
					if err := mem.Save(ctx, []llm.Message{msg}); err != nil {
						errs <- err
					}
				}
			})
		}
	}

	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		failed++

		if !errors.Is(err, natsmemory.ErrConcurrentUpdate) {
			t.Fatalf("Save: %v", err)
		}
	}

	t.Logf("%d of %d saves gave up with ErrConcurrentUpdate", failed, processes*writers*saves)

	got, err := mems[0].Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	// A Save that gave up stored nothing; every other one must be there.
	if want := processes*writers*saves - failed; len(got) != want {
		t.Fatalf("stored %d messages, want %d (%d saves gave up)", len(got), want, failed)
	}

	seen := make(map[string]bool, len(got))
	for _, m := range got {
		if seen[m.TextContent()] {
			t.Fatalf("message %q stored twice", m.TextContent())
		}

		seen[m.TextContent()] = true
	}
}

// TestNATSMemorySessionTooLarge is issue #8: a session that no longer fits in
// one NATS message fails with ErrSessionTooLarge, whichever limit it hits — the
// bucket's MaxValueSize (refused by the server) or the connection's
// max_payload (refused by nats.go before sending).
func TestNATSMemorySessionTooLarge(t *testing.T) {
	nc := requireNATS(t)
	ctx := context.Background()

	const bucket = "phero-e2e-memsize"

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}

	t.Cleanup(func() { _ = js.DeleteKeyValue(bucket) })

	kvStore, err := js.CreateKeyValue(&natsio.KeyValueConfig{Bucket: bucket, MaxValueSize: 4096})
	if err != nil {
		t.Fatalf("CreateKeyValue: %v", err)
	}

	cases := map[string]int{
		"bucket MaxValueSize":    8 * 1024,
		"connection max_payload": int(nc.MaxPayload()) + 1024,
	}

	for name, size := range cases {
		t.Run(name, func(t *testing.T) {
			mem, err := natsmemory.New(kvStore, "big-"+uuid.NewString())
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if err = mem.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("small"))}); err != nil {
				t.Fatalf("Save small: %v", err)
			}

			big := llm.UserMessage(llm.Text(strings.Repeat("x", size)))

			err = mem.Save(ctx, []llm.Message{big})
			if !errors.Is(err, natsmemory.ErrSessionTooLarge) {
				t.Fatalf("Save big = %v, want ErrSessionTooLarge", err)
			}

			// The history before the failed Save is intact.
			got, err := mem.Retrieve(ctx, "")
			if err != nil || len(got) != 1 {
				t.Fatalf("Retrieve = %d messages, %v; want the 1 saved before", len(got), err)
			}
		})
	}
}
