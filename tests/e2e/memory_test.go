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
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	memoryjsonfile "github.com/henomis/phero/v2/memory/jsonfile"
	memorypsql "github.com/henomis/phero/v2/memory/psql"
	memorysimple "github.com/henomis/phero/v2/memory/simple"
)

// TestMemorySimple_SaveRetrieveClear exercises the full lifecycle of the
// in-memory simple memory backend.
func TestMemorySimple_SaveRetrieveClear(t *testing.T) {
	ctx := context.Background()

	mem := memorysimple.New(10)

	messages := []llm.Message{
		llm.UserMessage(llm.Text("Hello, I am Alice.")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("Hello Alice!")}, llm.ToolCall{}),
	}

	if err := mem.Save(ctx, messages); err != nil {
		t.Fatalf("Save: %v", err)
	}

	retrieved, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if len(retrieved) == 0 {
		t.Fatal("expected non-empty retrieved messages")
	}

	// Verify the first saved message is there.
	found := false

	for _, m := range retrieved {
		if strings.Contains(m.TextContent(), "Alice") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected to find 'Alice' in retrieved messages, got: %v", retrieved)
	}

	// Clear and verify empty.
	if err := mem.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	retrieved, err = mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve after Clear: %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("expected empty memory after Clear, got %d messages", len(retrieved))
	}
}

// TestMemorySimple_BoundedCapacity verifies that the simple memory discards
// old messages once it reaches capacity.
func TestMemorySimple_BoundedCapacity(t *testing.T) {
	ctx := context.Background()
	const capacity = 3

	mem := memorysimple.New(capacity)

	for i := range 10 {
		msg := []llm.Message{
			llm.UserMessage(llm.Text("message " + string(rune('A'+i)))),
		}

		if err := mem.Save(ctx, msg); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	retrieved, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if uint(len(retrieved)) > capacity {
		t.Errorf("expected at most %d messages, got %d", capacity, len(retrieved))
	}
}

// TestMemorySimple_Summarization verifies that summarization is triggered
// when the buffer threshold is exceeded.
func TestMemorySimple_Summarization(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	llmClient := buildOpenAILLM()

	// summarizeThreshold=4, summarySize=2
	mem := memorysimple.New(10, memorysimple.WithSummarization(llmClient, 4, 2))

	// Save enough messages to trigger summarisation.
	for i := range 6 {
		msgs := []llm.Message{
			llm.UserMessage(llm.Text("Turn " + string(rune('A'+i)) + ": some content.")),
		}

		if err := mem.Save(ctx, msgs); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}

	retrieved, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	t.Logf("After summarization: %d messages in memory", len(retrieved))
}

// TestMemoryJSONFile_SaveRetrieveClear exercises the JSON-file memory backend.
func TestMemoryJSONFile_SaveRetrieveClear(t *testing.T) {
	ctx := context.Background()

	tmpFile := t.TempDir() + "/memory.json"

	mem, err := memoryjsonfile.New(tmpFile)
	if err != nil {
		t.Fatalf("jsonfile.New: %v", err)
	}

	messages := []llm.Message{
		llm.UserMessage(llm.Text("Hello I am Charlie.")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("Hello Charlie!")}, llm.ToolCall{}),
	}

	if err := mem.Save(ctx, messages); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload from file to verify persistence.
	mem2, err := memoryjsonfile.New(tmpFile)
	if err != nil {
		t.Fatalf("jsonfile.New reload: %v", err)
	}

	retrieved, err := mem2.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if len(retrieved) == 0 {
		t.Fatal("expected messages to persist in JSON file")
	}

	found := false

	for _, m := range retrieved {
		if strings.Contains(m.TextContent(), "Charlie") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected 'Charlie' in persisted messages, got: %v", retrieved)
	}

	if err := mem2.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	retrieved, err = mem2.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve after Clear: %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("expected empty memory after Clear, got %d messages", len(retrieved))
	}
}

// TestMemoryJSONFile_Persistence verifies that messages survive a full
// close-and-reopen cycle of the JSON file memory.
func TestMemoryJSONFile_Persistence(t *testing.T) {
	ctx := context.Background()

	tmpFile := t.TempDir() + "/persist.json"

	// Write phase.
	{
		mem, err := memoryjsonfile.New(tmpFile)
		if err != nil {
			t.Fatalf("first open: %v", err)
		}

		if err := mem.Save(ctx, []llm.Message{
			llm.UserMessage(llm.Text("persistent data")),
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	// Read phase using a fresh instance.
	{
		mem, err := memoryjsonfile.New(tmpFile)
		if err != nil {
			t.Fatalf("second open: %v", err)
		}

		msgs, err := mem.Retrieve(ctx, "")
		if err != nil {
			t.Fatalf("Retrieve: %v", err)
		}

		if len(msgs) == 0 {
			t.Fatal("messages did not persist")
		}

		if !strings.Contains(msgs[0].TextContent(), "persistent data") {
			t.Errorf("unexpected content: %q", msgs[0].TextContent())
		}
	}
}

// TestMemoryPostgres_SaveRetrieveClear exercises the PostgreSQL memory backend.
// Requires a running PostgreSQL instance (skipped otherwise).
func TestMemoryPostgres_SaveRetrieveClear(t *testing.T) {
	db := requirePostgres(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sessionID := "e2e-test-" + uuid.New().String()

	mem, err := memorypsql.New(db, sessionID,
		memorypsql.WithEnsureSchema(true),
	)
	if err != nil {
		t.Fatalf("psql.New: %v", err)
	}

	messages := []llm.Message{
		llm.UserMessage(llm.Text("Hi, I am Dave.")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("Hello Dave!")}, llm.ToolCall{}),
	}

	if err := mem.Save(ctx, messages); err != nil {
		t.Fatalf("Save: %v", err)
	}

	retrieved, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	if len(retrieved) == 0 {
		t.Fatal("expected non-empty messages from PostgreSQL")
	}

	found := false

	for _, m := range retrieved {
		if strings.Contains(m.TextContent(), "Dave") {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("expected 'Dave' in retrieved messages")
	}

	if err := mem.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	retrieved, err = mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve after Clear: %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("expected 0 messages after Clear, got %d", len(retrieved))
	}
}

// TestMemoryPostgres_SessionIsolation verifies that two sessions do not share
// messages.
func TestMemoryPostgres_SessionIsolation(t *testing.T) {
	db := requirePostgres(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sessionA := "e2e-session-a-" + uuid.New().String()
	sessionB := "e2e-session-b-" + uuid.New().String()

	memA, err := memorypsql.New(db, sessionA, memorypsql.WithEnsureSchema(true))
	if err != nil {
		t.Fatalf("psql.New A: %v", err)
	}

	memB, err := memorypsql.New(db, sessionB, memorypsql.WithEnsureSchema(true))
	if err != nil {
		t.Fatalf("psql.New B: %v", err)
	}

	if err := memA.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("session A only"))}); err != nil {
		t.Fatalf("memA.Save: %v", err)
	}

	if err := memB.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("session B only"))}); err != nil {
		t.Fatalf("memB.Save: %v", err)
	}

	retrievedA, err := memA.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("memA.Retrieve: %v", err)
	}

	for _, m := range retrievedA {
		if strings.Contains(m.TextContent(), "session B") {
			t.Error("session A received session B messages")
		}
	}

	retrievedB, err := memB.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("memB.Retrieve: %v", err)
	}

	for _, m := range retrievedB {
		if strings.Contains(m.TextContent(), "session A") {
			t.Error("session B received session A messages")
		}
	}
}

// TestMemoryPostgres_Snapshots covers the snapshot round trip, the
// compare-and-swap, session isolation and pruning.
func TestMemoryPostgres_Snapshots(t *testing.T) {
	db := requirePostgres(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	texts := func(prefix string, n int) []llm.Message {
		msgs := make([]llm.Message, 0, n)
		for i := range n {
			msgs = append(msgs, llm.UserMessage(llm.Text(fmt.Sprintf("%s%d", prefix, i))))
		}

		return msgs
	}

	for _, prune := range []bool{false, true} {
		session := "e2e-snap-" + uuid.New().String()

		mem, err := memorypsql.New(db, session, memorypsql.WithPruneCompacted(prune))
		if err != nil {
			t.Fatal(err)
		}

		other, err := memorypsql.New(db, session+"-other")
		if err != nil {
			t.Fatal(err)
		}

		if err = mem.Save(ctx, texts("m", 4)); err != nil {
			t.Fatal(err)
		}

		if err = other.Save(ctx, texts("o", 2)); err != nil {
			t.Fatal(err)
		}

		snap, logged, err := mem.LoadSnapshot(ctx)
		if err != nil || snap != nil || len(logged) != 4 {
			t.Fatalf("prune %v: LoadSnapshot = %v, %d, %v", prune, snap, len(logged), err)
		}

		if err = mem.SaveSnapshot(ctx, memory.Snapshot{Summary: "s1"}, logged[1].Cursor, nil); err != nil {
			t.Fatalf("prune %v: SaveSnapshot: %v", prune, err)
		}

		// A second writer that started from no snapshot loses.
		if err = mem.SaveSnapshot(ctx, memory.Snapshot{Summary: "x"}, logged[2].Cursor, nil); !errors.Is(err, memory.ErrSnapshotConflict) {
			t.Fatalf("prune %v: err = %v, want ErrSnapshotConflict", prune, err)
		}

		// Another session's cursor is not this session's.
		_, otherLogged, _ := other.LoadSnapshot(ctx)
		if err = mem.SaveSnapshot(ctx, memory.Snapshot{Summary: "x"}, otherLogged[1].Cursor, snap); !errors.Is(err, memory.ErrInvalidCursor) &&
			!errors.Is(err, memory.ErrSnapshotConflict) {
			t.Fatalf("prune %v: other session's cursor: err = %v", prune, err)
		}

		snap, logged, err = mem.LoadSnapshot(ctx)
		if err != nil || snap == nil || snap.Summary != "s1" || len(logged) != 2 || logged[0].Message.TextContent() != "m2" {
			t.Fatalf("prune %v: after the snapshot: %+v, %d messages, %v", prune, snap, len(logged), err)
		}

		view, err := mem.Retrieve(ctx, "")
		if err != nil || len(view) != 3 || !strings.Contains(view[0].TextContent(), "s1") {
			t.Fatalf("prune %v: Retrieve = %+v, %v", prune, view, err)
		}

		var rows int

		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM conversation_memory WHERE session_id = $1", session).Scan(&rows); err != nil {
			t.Fatal(err)
		}

		if want := map[bool]int{false: 4, true: 2}[prune]; rows != want {
			t.Fatalf("prune %v: %d rows, want %d", prune, rows, want)
		}

		if otherView, _ := other.Retrieve(ctx, ""); len(otherView) != 2 {
			t.Fatalf("prune %v: the other session sees %d messages, want its 2", prune, len(otherView))
		}

		_ = mem.Clear(ctx)
		_ = other.Clear(ctx)
	}
}

// TestMemoryPostgres_SaveWaitsForTheSessionsWriter checks that a Save waits
// while another transaction appends to the same session, so the session's
// rows commit in seq order.
func TestMemoryPostgres_SaveWaitsForTheSessionsWriter(t *testing.T) {
	db := requirePostgres(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const table = "conversation_memory"

	sessionID := "e2e-test-" + uuid.New().String()

	mem, err := memorypsql.New(db, sessionID, memorypsql.WithEnsureSchema(true), memorypsql.WithTable(table))
	if err != nil {
		t.Fatalf("psql.New: %v", err)
	}

	if err = mem.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Another writer holds the session's lock, as a Save in progress does.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1), hashtext($2))`, table, sessionID); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)

	go func() { done <- mem.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("after"))}) }()

	select {
	case err = <-done:
		t.Fatalf("Save returned (%v) while another writer held the session", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if err = <-done; err != nil {
		t.Fatalf("Save: %v", err)
	}
}
