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
	"sync"
	"testing"
	"time"

	natsmemory "github.com/henomis/phero/memory/nats"
	"github.com/henomis/phero/tool/kv"
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
