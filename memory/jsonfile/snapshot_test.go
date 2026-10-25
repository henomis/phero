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

package jsonfile_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	"github.com/henomis/phero/v2/memory/jsonfile"
)

var _ memory.SnapshotStore = (*jsonfile.Memory)(nil)

func saveTexts(t *testing.T, m *jsonfile.Memory, prefix string, n int) {
	t.Helper()

	msgs := make([]llm.Message, 0, n)
	for i := range n {
		msgs = append(msgs, llm.UserMessage(llm.Text(fmt.Sprintf("%s%d", prefix, i))))
	}

	if err := m.Save(context.Background(), msgs); err != nil {
		t.Fatal(err)
	}
}

func tailTexts(t *testing.T, m *jsonfile.Memory) (*memory.Snapshot, string) {
	t.Helper()

	snap, logged, err := m.LoadSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	texts := make([]string, 0, len(logged))
	for _, lm := range logged {
		texts = append(texts, lm.Message.TextContent())
	}

	return snap, strings.Join(texts, " ")
}

func TestSnapshot_ReopenKeepsTheSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")

	m, err := jsonfile.New(path)
	if err != nil {
		t.Fatal(err)
	}

	saveTexts(t, m, "m", 4)

	// No snapshot yet: the file stays a plain array, which older versions read.
	if data, _ := os.ReadFile(path); !strings.HasPrefix(strings.TrimSpace(string(data)), "[") {
		t.Fatalf("file = %s, want a JSON array", data)
	}

	_, logged, _ := m.LoadSnapshot(context.Background())
	if err = m.SaveSnapshot(context.Background(), memory.Snapshot{Summary: "s1"}, logged[1].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	reopened, err := jsonfile.New(path)
	if err != nil {
		t.Fatal(err)
	}

	snap, tail := tailTexts(t, reopened)
	if snap == nil || snap.Summary != "s1" || tail != "m2 m3" {
		t.Fatalf("after reopen: snapshot %+v, tail %q; want s1 and m2 m3", snap, tail)
	}

	// Cursors keep counting across the reopen.
	saveTexts(t, reopened, "n", 1)

	if err = reopened.SaveSnapshot(context.Background(), memory.Snapshot{Summary: "s2"}, logged[3].Cursor, snap); err != nil {
		t.Fatal(err)
	}

	if _, tail = tailTexts(t, reopened); tail != "n0" {
		t.Fatalf("tail = %q, want n0", tail)
	}

	view, err := reopened.Retrieve(context.Background(), "")
	if err != nil || len(view) != 2 || !strings.Contains(view[0].TextContent(), "s2") {
		t.Fatalf("Retrieve = %+v, %v; want the summary then n0", view, err)
	}
}

func TestSnapshot_LoadsVersion1Files(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")
	if err := os.WriteFile(path, []byte(`[{"role":"user","parts":[{"type":"text","text":"old"}]}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	m, err := jsonfile.New(path)
	if err != nil {
		t.Fatal(err)
	}

	if snap, tail := tailTexts(t, m); snap != nil || tail != "old" {
		t.Fatalf("snapshot %+v, tail %q; want the v1 messages and no snapshot", snap, tail)
	}
}

func TestSnapshot_PruneRewritesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")

	m, err := jsonfile.New(path, jsonfile.WithPruneCompacted(true))
	if err != nil {
		t.Fatal(err)
	}

	saveTexts(t, m, "m", 5)

	_, logged, _ := m.LoadSnapshot(context.Background())
	if err = m.SaveSnapshot(context.Background(), memory.Snapshot{Summary: "s"}, logged[2].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"m0"`) || strings.Contains(string(data), `"m2"`) || !strings.Contains(string(data), `"m3"`) {
		t.Fatalf("file = %s, want only the messages after the snapshot", data)
	}

	reopened, err := jsonfile.New(path)
	if err != nil {
		t.Fatal(err)
	}

	saveTexts(t, reopened, "n", 1)

	if _, tail := tailTexts(t, reopened); tail != "m3 m4 n0" {
		t.Fatalf("tail = %q", tail)
	}
}

func TestSnapshot_ConflictAndUnsupported(t *testing.T) {
	m, err := jsonfile.New(filepath.Join(t.TempDir(), "memory.json"))
	if err != nil {
		t.Fatal(err)
	}

	saveTexts(t, m, "m", 4)

	_, logged, _ := m.LoadSnapshot(context.Background())
	if err = m.SaveSnapshot(context.Background(), memory.Snapshot{Summary: "a"}, logged[0].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	if err = m.SaveSnapshot(context.Background(), memory.Snapshot{Summary: "b"}, logged[2].Cursor, nil); !errors.Is(err, memory.ErrSnapshotConflict) {
		t.Fatalf("err = %v, want ErrSnapshotConflict", err)
	}

	summarizing, err := jsonfile.New(filepath.Join(t.TempDir(), "s.json"), jsonfile.WithSummarization(llm.Func(nil), 10, 5))
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err = summarizing.LoadSnapshot(context.Background()); !errors.Is(err, memory.ErrSnapshotsUnsupported) {
		t.Fatalf("err = %v, want ErrSnapshotsUnsupported", err)
	}
}
