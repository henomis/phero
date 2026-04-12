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

package jsonfile

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
)

type mockSummaryLLM struct {
	called int
}

func (m *mockSummaryLLM) Execute(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
	m.called++
	return &llm.Result{Message: &llm.Message{Role: llm.RoleSystem, Parts: []llm.ContentPart{llm.Text("summary")}}}, nil
}

func TestMemory_SaveAndRetrieve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem.json")
	mem, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx := context.Background()
	msgs := []llm.Message{
		llm.UserMessage(llm.Text("hello")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("world")}),
	}
	if err := mem.Save(ctx, msgs); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(got) != len(msgs) {
		t.Fatalf("Retrieve() len = %d, want %d", len(got), len(msgs))
	}
	for i, m := range msgs {
		if got[i].Role != m.Role || got[i].TextContent() != m.TextContent() {
			t.Fatalf("message[%d] = {%s %q}, want {%s %q}", i, got[i].Role, got[i].TextContent(), m.Role, m.TextContent())
		}
	}
}

func TestMemory_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem.json")
	ctx := context.Background()

	mem1, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	msgs := []llm.Message{
		llm.UserMessage(llm.Text("persisted")),
	}
	if err := mem1.Save(ctx, msgs); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// open a second instance from the same file
	mem2, err := New(path)
	if err != nil {
		t.Fatalf("New() (reopen) error = %v", err)
	}
	got, err := mem2.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(got) != 1 || got[0].TextContent() != "persisted" {
		t.Fatalf("Retrieve() after reopen = %v, want [{persisted}]", got)
	}
}

func TestMemory_Clear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem.json")
	ctx := context.Background()

	mem, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := mem.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("x"))}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := mem.Clear(ctx); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	got, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve() after Clear error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Retrieve() after Clear = %d messages, want 0", len(got))
	}

	// also verify the file was re-written (reopen should return empty)
	mem2, err := New(path)
	if err != nil {
		t.Fatalf("New() (reopen after Clear) error = %v", err)
	}
	got2, err := mem2.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve() after reopen post-Clear error = %v", err)
	}
	if len(got2) != 0 {
		t.Fatalf("Retrieve() after reopen post-Clear = %d messages, want 0", len(got2))
	}
}

func TestMemory_ConcurrentSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem.json")
	ctx := context.Background()

	mem, err := New(path)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const goroutines = 20
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- mem.Save(ctx, []llm.Message{llm.UserMessage(llm.Text("concurrent"))})
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("Save() concurrent error = %v", err)
		}
	}

	got, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if len(got) != goroutines {
		t.Fatalf("Retrieve() len = %d, want %d", len(got), goroutines)
	}
}

func TestMemory_SummarizationReplacesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem.json")
	ctx := context.Background()

	mockLLM := &mockSummaryLLM{}
	mem, err := New(path, WithSummarization(mockLLM, 6, 4))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	msgs := []llm.Message{
		llm.UserMessage(llm.Text("hello1")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("hello2")}),
		llm.UserMessage(llm.Text("hello3")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("hello4")}),
		llm.UserMessage(llm.Text("hello5")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("hello6")}),
	}
	if err := mem.Save(ctx, msgs); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if mockLLM.called != 1 {
		t.Fatalf("summary LLM called %d times, want 1", mockLLM.called)
	}

	got, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	// Expect: [summary system msg, hello5, hello6]
	wantContents := []string{memory.SummarySystemMessagePrefix + "summary", "hello5", "hello6"}
	if len(got) != len(wantContents) {
		t.Fatalf("Retrieve() len = %d, want %d", len(got), len(wantContents))
	}
	for i, want := range wantContents {
		if got[i].TextContent() != want {
			t.Fatalf("message[%d].TextContent() = %q, want %q", i, got[i].TextContent(), want)
		}
	}
}

func TestMemory_SummarizationPersistedToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mem.json")
	ctx := context.Background()

	mockLLM := &mockSummaryLLM{}
	mem, err := New(path, WithSummarization(mockLLM, 4, 2))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	msgs := []llm.Message{
		llm.UserMessage(llm.Text("a")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("b")}),
		llm.UserMessage(llm.Text("c")),
		llm.AssistantMessage([]llm.ContentPart{llm.Text("d")}),
	}
	if err := mem.Save(ctx, msgs); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Reopen and confirm the summarized state was written to disk
	mem2, err := New(path)
	if err != nil {
		t.Fatalf("New() reopen error = %v", err)
	}
	got, err := mem2.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve() after reopen error = %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Retrieve() after reopen returned empty slice")
	}
	if !strings.HasPrefix(got[0].TextContent(), memory.SummarySystemMessagePrefix) {
		t.Fatalf("first message after reopen = %q, want summary prefix %q", got[0].TextContent(), memory.SummarySystemMessagePrefix)
	}
}
