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


package natsmemory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
)

var _ memory.SnapshotStore = (*Memory)(nil)

func userTexts(prefix string, n int) []llm.Message {
	msgs := make([]llm.Message, 0, n)
	for i := range n {
		msgs = append(msgs, llm.UserMessage(llm.Text(fmt.Sprintf("%s%d", prefix, i))))
	}

	return msgs
}

func loggedTexts(logged []memory.LoggedMessage) string {
	out := make([]string, 0, len(logged))
	for _, lm := range logged {
		out = append(out, lm.Message.TextContent())
	}

	return strings.Join(out, " ")
}

func TestSnapshot_RoundTripAndArrayValues(t *testing.T) {
	ctx := context.Background()
	kv := &fakeKV{}
	m := newTestMemory(t, kv, WithPruneCompacted(false))

	// An array value, as older versions wrote it, loads.
	kv.appendDirect(t, userTexts("m", 4)...)

	snap, logged, err := m.LoadSnapshot(ctx)
	if err != nil || snap != nil || loggedTexts(logged) != "m0 m1 m2 m3" {
		t.Fatalf("LoadSnapshot = %v, %q, %v", snap, loggedTexts(logged), err)
	}

	if err = m.SaveSnapshot(ctx, memory.Snapshot{Summary: "s1"}, logged[1].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	if err = m.Save(ctx, userTexts("n", 1)); err != nil {
		t.Fatal(err)
	}

	snap, logged, err = m.LoadSnapshot(ctx)
	if err != nil || snap == nil || snap.Summary != "s1" || loggedTexts(logged) != "m2 m3 n0" {
		t.Fatalf("after the snapshot: %+v, %q, %v", snap, loggedTexts(logged), err)
	}

	view, err := m.Retrieve(ctx, "")
	if err != nil || len(view) != 4 || !strings.Contains(view[0].TextContent(), "s1") {
		t.Fatalf("Retrieve = %+v, %v", view, err)
	}

	// A writer that started from no snapshot loses.
	if err = m.SaveSnapshot(ctx, memory.Snapshot{Summary: "x"}, logged[1].Cursor, nil); !errors.Is(err, memory.ErrSnapshotConflict) {
		t.Fatalf("err = %v, want ErrSnapshotConflict", err)
	}
}

func TestSnapshot_PruneByDefaultKeepsTheValueSmall(t *testing.T) {
	ctx := context.Background()
	kv := &fakeKV{}
	m := newTestMemory(t, kv) // pruning is on by default for NATS

	big := strings.Repeat("x", 1000)

	for round := range 20 {
		msgs := []llm.Message{llm.UserMessage(llm.Text(fmt.Sprintf("r%d %s", round, big)))}
		if err := m.Save(ctx, msgs); err != nil {
			t.Fatal(err)
		}

		snap, logged, err := m.LoadSnapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if err = m.SaveSnapshot(ctx, memory.Snapshot{Summary: "so far"}, logged[len(logged)-1].Cursor, snap); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}

	// Twenty 1 kB messages were saved; the value holds the snapshot only.
	if len(kv.value) > 1000 {
		t.Fatalf("value is %d bytes, want the covered messages pruned", len(kv.value))
	}

	if err := m.Save(ctx, userTexts("after", 1)); err != nil {
		t.Fatal(err)
	}

	if _, logged, _ := m.LoadSnapshot(ctx); loggedTexts(logged) != "after0" {
		t.Fatalf("tail = %q, want the message saved after the last snapshot", loggedTexts(logged))
	}
}

func TestSnapshot_LostRaceRetries(t *testing.T) {
	ctx := context.Background()
	kv := &fakeKV{}
	m := newTestMemory(t, kv, WithPruneCompacted(false))

	if err := m.Save(ctx, userTexts("m", 3)); err != nil {
		t.Fatal(err)
	}

	_, logged, _ := m.LoadSnapshot(ctx)

	// Another process appends between SaveSnapshot's read and its write.
	raced := false
	kv.beforeWrite = func(kv *fakeKV) {
		if !raced {
			raced = true

			kv.appendDirect(t, llm.UserMessage(llm.Text("other")))
		}
	}

	if err := m.SaveSnapshot(ctx, memory.Snapshot{Summary: "s"}, logged[0].Cursor, nil); err != nil {
		t.Fatalf("SaveSnapshot after a lost race: %v", err)
	}

	kv.beforeWrite = nil

	if _, tail, _ := m.LoadSnapshot(ctx); loggedTexts(tail) != "m1 m2 other" {
		t.Fatalf("tail = %q, want the other writer's message kept and the cutoff unmoved", loggedTexts(tail))
	}
}

func TestSnapshot_UnsupportedWithSummarization(t *testing.T) {
	m := newTestMemory(t, &fakeKV{}, WithSummarization(llm.Func(nil), 10, 5))

	if _, _, err := m.LoadSnapshot(context.Background()); !errors.Is(err, memory.ErrSnapshotsUnsupported) {
		t.Fatalf("err = %v, want ErrSnapshotsUnsupported", err)
	}
}
