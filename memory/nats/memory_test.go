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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/llm"
)

// fakeEntry is the part of nats.KeyValueEntry that Memory reads.
type fakeEntry struct {
	nats.KeyValueEntry

	value    []byte
	revision uint64
}

func (e fakeEntry) Value() []byte              { return e.value }
func (e fakeEntry) Revision() uint64           { return e.revision }
func (e fakeEntry) Key() string                { return "" }
func (e fakeEntry) Bucket() string             { return "" }
func (e fakeEntry) Operation() nats.KeyValueOp { return nats.KeyValuePut }

// fakeKV is a single-key nats.KeyValue with JetStream's revision semantics:
// Create and Update fail with nats.ErrKeyExists when the key has moved on.
type fakeKV struct {
	nats.KeyValue

	value    []byte
	revision uint64 // last sequence of the key; kept across a purge, as in JetStream
	present  bool

	// beforeWrite, when set, runs before each Create or Update, so a test can
	// slip in another writer between Save's read and its write.
	beforeWrite func(kv *fakeKV)
	writes      int

	// maxValue, when set, makes a larger write fail with tooLargeErr, the way
	// nats.go or the server refuses a value over its limit.
	maxValue    int
	tooLargeErr error
}

func (kv *fakeKV) Get(string) (nats.KeyValueEntry, error) {
	if !kv.present {
		return nil, nats.ErrKeyNotFound
	}

	return fakeEntry{value: kv.value, revision: kv.revision}, nil
}

func (kv *fakeKV) Create(_ string, value []byte) (uint64, error) {
	kv.hook()

	if kv.maxValue > 0 && len(value) > kv.maxValue {
		return 0, kv.tooLargeErr
	}

	if kv.present {
		return 0, nats.ErrKeyExists
	}

	return kv.put(value), nil
}

func (kv *fakeKV) Update(_ string, value []byte, last uint64) (uint64, error) {
	kv.hook()

	if kv.maxValue > 0 && len(value) > kv.maxValue {
		return 0, kv.tooLargeErr
	}

	if !kv.present || kv.revision != last {
		return 0, nats.ErrKeyExists
	}

	return kv.put(value), nil
}

func (kv *fakeKV) Purge(string, ...nats.DeleteOpt) error {
	kv.present = false
	kv.value = nil
	kv.revision++

	return nil
}

func (kv *fakeKV) hook() {
	kv.writes++

	if kv.beforeWrite != nil {
		kv.beforeWrite(kv)
	}
}

func (kv *fakeKV) put(value []byte) uint64 {
	kv.value = value
	kv.present = true
	kv.revision++

	return kv.revision
}

// appendDirect stores msgs after the current history, as another process
// would, without going through the hook.
func (kv *fakeKV) appendDirect(t *testing.T, msgs ...llm.Message) {
	t.Helper()

	var existing []llm.Message
	if kv.present {
		if err := json.Unmarshal(kv.value, &existing); err != nil {
			t.Fatalf("decode stored history: %v", err)
		}
	}

	data, err := json.Marshal(append(existing, msgs...))
	if err != nil {
		t.Fatalf("encode history: %v", err)
	}

	kv.put(data)
}

func texts(msgs []llm.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.TextContent()
	}

	return out
}

func newTestMemory(t *testing.T, kv *fakeKV, opts ...Option) *Memory {
	t.Helper()

	m, err := New(kv, "session-1", opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return m
}

// TestSave_LostRaceKeepsBothWriters is issue #2: another process writes between
// Save's read and its write. The write must not overwrite it.
func TestSave_LostRaceKeepsBothWriters(t *testing.T) {
	kv := &fakeKV{}
	m := newTestMemory(t, kv)
	ctx := context.Background()

	if err := m.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("first"))}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raced := false
	kv.beforeWrite = func(kv *fakeKV) {
		if !raced {
			raced = true

			kv.appendDirect(t, llm.UserMessage(llm.Text("other process")))
		}
	}

	if err := m.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("mine"))}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := m.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	want := []string{"first", "other process", "mine"}
	if gotTexts := texts(got); len(gotTexts) != len(want) || gotTexts[0] != want[0] ||
		gotTexts[1] != want[1] || gotTexts[2] != want[2] {
		t.Fatalf("history = %q, want %q", gotTexts, want)
	}
}

// TestSave_FirstWriteRace covers the Create path: another process creates the
// session between Save's read (no key) and its write.
func TestSave_FirstWriteRace(t *testing.T) {
	kv := &fakeKV{}
	m := newTestMemory(t, kv)

	raced := false
	kv.beforeWrite = func(kv *fakeKV) {
		if !raced {
			raced = true

			kv.appendDirect(t, llm.UserMessage(llm.Text("other process")))
		}
	}

	if err := m.Save(context.Background(), []llm.Message{llm.UserMessage(llm.Text("mine"))}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := texts(mustRetrieve(t, m)); len(got) != 2 || got[0] != "other process" || got[1] != "mine" {
		t.Fatalf("history = %q, want [other process mine]", got)
	}
}

// TestSave_GivesUpWithErrConcurrentUpdate verifies that a writer that loses
// every race fails with ErrConcurrentUpdate instead of retrying forever, and
// leaves the other writers' history intact.
func TestSave_GivesUpWithErrConcurrentUpdate(t *testing.T) {
	kv := &fakeKV{}
	m := newTestMemory(t, kv)

	kv.beforeWrite = func(kv *fakeKV) {
		kv.appendDirect(t, llm.UserMessage(llm.Text("other process")))
	}

	err := m.Save(context.Background(), []llm.Message{llm.UserMessage(llm.Text("mine"))})
	if !errors.Is(err, ErrConcurrentUpdate) {
		t.Fatalf("Save = %v, want ErrConcurrentUpdate", err)
	}

	if !errors.Is(err, nats.ErrKeyExists) {
		t.Fatalf("Save = %v, want it to wrap the last conflict", err)
	}

	if kv.writes != saveAttempts {
		t.Fatalf("write attempts = %d, want %d", kv.writes, saveAttempts)
	}

	for _, text := range texts(mustRetrieve(t, m)) {
		if text == "mine" {
			t.Fatal("a failed Save must not have stored its messages")
		}
	}
}

// TestSave_AfterClear verifies that a session cleared by Purge is written again
// through Create, even though the key's revision did not go back to 0.
func TestSave_AfterClear(t *testing.T) {
	kv := &fakeKV{}
	m := newTestMemory(t, kv)
	ctx := context.Background()

	if err := m.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("old"))}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := m.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	if err := m.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("new"))}); err != nil {
		t.Fatalf("Save after Clear: %v", err)
	}

	if got := texts(mustRetrieve(t, m)); len(got) != 1 || got[0] != "new" {
		t.Fatalf("history = %q, want [new]", got)
	}
}

// TestSave_RetryReusesSummary verifies that a retried Save does not call the
// summary LLM again when the messages it summarizes have not changed.
func TestSave_RetryReusesSummary(t *testing.T) {
	kv := &fakeKV{}

	calls := 0
	summarizer := llm.Func(func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
		calls++

		msg := llm.AssistantMessage([]llm.ContentPart{llm.Text("summary")})

		return &llm.Result{Message: &msg}, nil
	})

	m := newTestMemory(t, kv, WithSummarization(summarizer, 4, 2))

	kv.appendDirect(t,
		llm.UserMessage(llm.Text("m1")),
		llm.UserMessage(llm.Text("m2")),
		llm.UserMessage(llm.Text("m3")),
	)

	raced := false
	kv.beforeWrite = func(kv *fakeKV) {
		if !raced {
			raced = true

			kv.appendDirect(t, llm.UserMessage(llm.Text("other process")))
		}
	}

	if err := m.Save(context.Background(), []llm.Message{llm.UserMessage(llm.Text("mine"))}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if calls != 1 {
		t.Fatalf("summary LLM calls = %d, want 1", calls)
	}

	got := texts(mustRetrieve(t, m))
	if len(got) != 4 || got[2] != "other process" || got[3] != "mine" {
		t.Fatalf("history = %q, want summary, m3, other process, mine", got)
	}
}

func mustRetrieve(t *testing.T, m *Memory) []llm.Message {
	t.Helper()

	got, err := m.Retrieve(context.Background(), "")
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}

	return got
}

// TestSave_TooLarge is issue #8: a session over NATS's size limit fails with a
// clear error naming it, from either place NATS enforces the limit, without
// retrying.
func TestSave_TooLarge(t *testing.T) {
	cases := map[string]error{
		"connection max_payload": nats.ErrMaxPayload,
		"bucket MaxValueSize":    &nats.APIError{Code: 400, ErrorCode: jsErrMessageTooLarge, Description: "message size exceeds maximum allowed"},
	}

	for name, natsErr := range cases {
		t.Run(name, func(t *testing.T) {
			kv := &fakeKV{maxValue: 100, tooLargeErr: natsErr}
			m := newTestMemory(t, kv)

			big := llm.UserMessage(llm.Text(strings.Repeat("x", 200)))

			err := m.Save(context.Background(), []llm.Message{big})
			if !errors.Is(err, ErrSessionTooLarge) {
				t.Fatalf("Save = %v, want ErrSessionTooLarge", err)
			}

			tle, ok := errors.AsType[*SessionTooLargeError](err)
			if !ok || tle.Session != "session-1" || tle.Size <= 100 {
				t.Fatalf("SessionTooLargeError = %+v", tle)
			}

			if !errors.Is(err, natsErr) {
				t.Fatalf("Save = %v, want the NATS error still reachable", err)
			}

			if kv.writes != 1 {
				t.Fatalf("write attempts = %d, want 1: a too-large session is not retried", kv.writes)
			}
		})
	}
}
