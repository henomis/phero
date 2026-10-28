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

package simple_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	"github.com/henomis/phero/v2/memory/simple"
)

var _ memory.SnapshotStore = (*simple.Memory)(nil)

func texts(n int, prefix string) []llm.Message {
	msgs := make([]llm.Message, 0, n)
	for i := range n {
		msgs = append(msgs, llm.UserMessage(llm.Text(fmt.Sprintf("%s%d", prefix, i))))
	}

	return msgs
}

func loggedTexts(msgs []memory.LoggedMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.Message.TextContent())
	}

	return out
}

func TestSnapshot_LoadReturnsSnapshotAndTail(t *testing.T) {
	ctx := context.Background()
	m := simple.New(100)

	if err := m.Save(ctx, texts(5, "m")); err != nil {
		t.Fatal(err)
	}

	snap, logged, err := m.LoadSnapshot(ctx)
	if err != nil || snap != nil || len(logged) != 5 {
		t.Fatalf("LoadSnapshot = %v, %d messages, %v; want no snapshot and the 5 messages", snap, len(logged), err)
	}

	// The snapshot stands for m0..m2.
	if err = m.SaveSnapshot(ctx, memory.Snapshot{Summary: "s1"}, logged[2].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	snap, logged, err = m.LoadSnapshot(ctx)
	if err != nil || snap == nil || snap.Summary != "s1" || snap.UpTo == "" {
		t.Fatalf("LoadSnapshot = %+v, %v", snap, err)
	}

	if got := fmt.Sprint(loggedTexts(logged)); got != "[m3 m4]" {
		t.Fatalf("tail = %s, want [m3 m4]", got)
	}

	// Retrieve gives plain callers the short view too.
	view, err := m.Retrieve(ctx, "")
	if err != nil || len(view) != 3 || view[0].Role != llm.RoleUser || view[1].TextContent() != "m3" {
		t.Fatalf("Retrieve = %+v, %v; want the summary then m3, m4", view, err)
	}
}

func TestSnapshot_CursorSurvivesEvictionAndAppends(t *testing.T) {
	ctx := context.Background()
	m := simple.New(4)

	if err := m.Save(ctx, texts(4, "a")); err != nil {
		t.Fatal(err)
	}

	_, logged, _ := m.LoadSnapshot(ctx)
	upTo := logged[1].Cursor // a1

	// Another writer appends, and the buffer evicts a0 and a1.
	if err := m.Save(ctx, texts(2, "b")); err != nil {
		t.Fatal(err)
	}

	if err := m.SaveSnapshot(ctx, memory.Snapshot{Summary: "s"}, upTo, nil); err != nil {
		t.Fatal(err)
	}

	_, logged, _ = m.LoadSnapshot(ctx)
	if got := fmt.Sprint(loggedTexts(logged)); got != "[a2 a3 b0 b1]" {
		t.Fatalf("tail = %s, want the cutoff after a1, not moved by the appends", got)
	}
}

func TestSnapshot_StalePrevConflicts(t *testing.T) {
	ctx := context.Background()
	m := simple.New(100)

	if err := m.Save(ctx, texts(6, "m")); err != nil {
		t.Fatal(err)
	}

	_, logged, _ := m.LoadSnapshot(ctx)

	if err := m.SaveSnapshot(ctx, memory.Snapshot{Summary: "first"}, logged[1].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	// A second writer that also started from no snapshot loses.
	if err := m.SaveSnapshot(ctx, memory.Snapshot{Summary: "second"}, logged[3].Cursor, nil); !errors.Is(err, memory.ErrSnapshotConflict) {
		t.Fatalf("err = %v, want ErrSnapshotConflict", err)
	}

	first, _, _ := m.LoadSnapshot(ctx)

	// A cutoff must move past the previous one, and name a stored message.
	for _, upTo := range []memory.Cursor{logged[0].Cursor, "99", "not-a-cursor"} {
		if err := m.SaveSnapshot(ctx, memory.Snapshot{Summary: "x"}, upTo, first); !errors.Is(err, memory.ErrInvalidCursor) {
			t.Errorf("upTo %q: err = %v, want ErrInvalidCursor", upTo, err)
		}
	}

	if err := m.SaveSnapshot(ctx, memory.Snapshot{Summary: "next"}, logged[3].Cursor, first); err != nil {
		t.Fatalf("advancing from the latest snapshot: %v", err)
	}
}

func TestSnapshot_PruneCompacted(t *testing.T) {
	ctx := context.Background()

	for _, prune := range []bool{false, true} {
		m := simple.New(100, simple.WithPruneCompacted(prune))
		if err := m.Save(ctx, texts(5, "m")); err != nil {
			t.Fatal(err)
		}

		_, logged, _ := m.LoadSnapshot(ctx)
		if err := m.SaveSnapshot(ctx, memory.Snapshot{Summary: "s"}, logged[2].Cursor, nil); err != nil {
			t.Fatal(err)
		}

		want := 5
		if prune {
			want = 2
		}

		if m.Len() != want {
			t.Errorf("prune %v: %d messages stored, want %d", prune, m.Len(), want)
		}

		// Either way, the view is the same, and later saves keep their cursors.
		if err := m.Save(ctx, texts(1, "n")); err != nil {
			t.Fatal(err)
		}

		if _, tail, _ := m.LoadSnapshot(ctx); fmt.Sprint(loggedTexts(tail)) != "[m3 m4 n0]" {
			t.Errorf("prune %v: tail = %v", prune, loggedTexts(tail))
		}
	}
}

func TestSnapshot_UnsupportedWithSummarization(t *testing.T) {
	ctx := context.Background()
	m := simple.New(100, simple.WithSummarization(llm.Func(nil), 10, 5))

	if _, _, err := m.LoadSnapshot(ctx); !errors.Is(err, memory.ErrSnapshotsUnsupported) {
		t.Fatalf("LoadSnapshot err = %v, want ErrSnapshotsUnsupported", err)
	}

	if err := m.SaveSnapshot(ctx, memory.Snapshot{}, "0", nil); !errors.Is(err, memory.ErrSnapshotsUnsupported) {
		t.Fatalf("SaveSnapshot err = %v, want ErrSnapshotsUnsupported", err)
	}
}
