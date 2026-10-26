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
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	"github.com/henomis/phero/v2/memory/jsonfile"
)

func TestMeta_PersistsAndClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.json")

	m, err := jsonfile.New(path)
	if err != nil {
		t.Fatal(err)
	}

	if m.Meta() != nil {
		t.Fatalf("Meta = %v, want nil for a new file", m.Meta())
	}

	if err = m.SetMeta("title", "fix the test"); err != nil {
		t.Fatal(err)
	}

	if err = m.SetMeta("model", "opus"); err != nil {
		t.Fatal(err)
	}

	if err = m.SetMeta("model", ""); err != nil {
		t.Fatal(err)
	}

	got := m.Meta()
	got["title"] = "changed through the copy"

	reopened, err := jsonfile.New(path)
	if err != nil {
		t.Fatal(err)
	}

	if want := map[string]string{"title": "fix the test"}; !maps.Equal(reopened.Meta(), want) {
		t.Fatalf("Meta after reopen = %v, want %v", reopened.Meta(), want)
	}

	if err = reopened.Clear(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Without a snapshot or metadata, the file is no longer the version 2 object.
	if data, _ := os.ReadFile(path); strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		t.Fatalf("file after Clear = %s, want the version 1 format", data)
	}

	if reopened.Meta() != nil {
		t.Fatalf("Meta after Clear = %v, want nil", reopened.Meta())
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	m, err := jsonfile.New(path, jsonfile.WithPruneCompacted(true))
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().Add(-time.Second)

	if err = m.Save(context.Background(), []llm.Message{
		llm.UserMessage(llm.Text("fix the failing test")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("on it")}),
		llm.UserMessage(llm.Text("thanks")),
	}); err != nil {
		t.Fatal(err)
	}

	info, err := jsonfile.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}

	if info.Messages != 3 || info.FirstUserText != "fix the failing test" || info.Compacted || info.Meta != nil ||
		info.UpdatedAt.Before(before) {
		t.Fatalf("Info = %+v, want 3 messages, the first user text, no snapshot", info)
	}

	if err = m.SetMeta("title", "failing test"); err != nil {
		t.Fatal(err)
	}

	_, logged, _ := m.LoadSnapshot(context.Background())
	if err = m.SaveSnapshot(context.Background(), memory.Snapshot{Summary: "s"}, logged[1].Cursor, nil); err != nil {
		t.Fatal(err)
	}

	// The first two messages are pruned: the count keeps them, the text is gone.
	if info, err = jsonfile.Inspect(path); err != nil {
		t.Fatal(err)
	}

	if info.Messages != 3 || info.FirstUserText != "thanks" || !info.Compacted || info.Meta["title"] != "failing test" {
		t.Fatalf("Info after pruning = %+v", info)
	}

	if _, err = jsonfile.Inspect(filepath.Join(dir, "missing.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Inspect(missing) err = %v, want os.ErrNotExist", err)
	}

	future := filepath.Join(dir, "future.json")
	if err = os.WriteFile(future, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err = jsonfile.Inspect(future); !errors.Is(err, jsonfile.ErrUnknownFileVersion) {
		t.Fatalf("Inspect(future) err = %v, want ErrUnknownFileVersion", err)
	}
}
