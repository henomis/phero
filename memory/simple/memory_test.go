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

package simple

import (
	"context"
	"testing"

	"github.com/henomis/phero/v2/llm"
)

type mockSummaryLLM struct {
	called int
}

func (m *mockSummaryLLM) Execute(_ context.Context, _ []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
	m.called++
	return &llm.Result{Message: &llm.Message{Role: llm.RoleSystem, Parts: []llm.ContentPart{llm.Text("summary")}}}, nil
}

func TestMemorySave_SummarizationReplacesHistory(t *testing.T) {
	ctx := context.Background()

	mockLLM := &mockSummaryLLM{}
	mem := New(10, WithSummarization(mockLLM, 6, 4))

	msgs := []llm.Message{
		{Role: llm.RoleUser, Parts: []llm.ContentPart{llm.Text("hello1")}},
		{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("hello2")}},
		{Role: llm.RoleTool, Parts: []llm.ContentPart{llm.Text("hello3")}},
		{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("hello4")}},
		{Role: llm.RoleUser, Parts: []llm.ContentPart{llm.Text("hello5")}},
		{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("hello6")}},
	}

	if err := mem.Save(ctx, msgs); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	if mockLLM.called != 1 {
		t.Fatalf("expected summary LLM to be called once, got %d", mockLLM.called)
	}

	got, err := mem.Retrieve(ctx, "")
	if err != nil {
		t.Fatalf("Retrieve returned error: %v", err)
	}

	wantContents := []string{"Summary of previous conversation:\nsummary", "hello5", "hello6"}
	if len(got) != len(wantContents) {
		t.Fatalf("expected %d messages in buffer, got %d", len(wantContents), len(got))
	}

	for i := range wantContents {
		if got[i].TextContent() != wantContents[i] {
			t.Fatalf("buffer[%d].TextContent(): expected %q, got %q", i, wantContents[i], got[i].TextContent())
		}
	}
}

// toolTurn returns an assistant message calling tool id and its result.
func toolTurn(id string) []llm.Message {
	return []llm.Message{
		llm.AssistantMessage(nil, llm.ToolCall{ID: id, Function: llm.FunctionCall{Name: "f", Arguments: "{}"}}),
		llm.ToolResultMessage(id, llm.Text("result "+id)),
	}
}

// assertNoOrphans fails when a tool result's call is not before it.
func assertNoOrphans(t *testing.T, msgs []llm.Message) {
	t.Helper()

	calls := map[string]bool{}

	for i, m := range msgs {
		for _, tc := range m.ToolCalls {
			calls[tc.ID] = true
		}

		if m.Role == llm.RoleTool && !calls[m.ToolCallID] {
			t.Fatalf("message %d answers tool call %q, which is not before it", i, m.ToolCallID)
		}
	}
}

func TestMemorySave_SummarizationKeepsToolPairs(t *testing.T) {
	ctx := context.Background()

	// Summarizing the oldest 2 messages would split the first tool turn.
	mem := New(20, WithSummarization(&mockSummaryLLM{}, 5, 2))

	msgs := append([]llm.Message{llm.UserMessage(llm.Text("go"))}, toolTurn("a")...)
	msgs = append(msgs, toolTurn("b")...)

	if err := mem.Save(ctx, msgs); err != nil {
		t.Fatal(err)
	}

	got, _ := mem.Retrieve(ctx, "")
	assertNoOrphans(t, got)

	if len(got) != 3 || got[1].ToolCalls[0].ID != "b" {
		t.Fatalf("got %d messages, want summary + tool turn b", len(got))
	}
}

func TestMemoryRetrieve_EvictionDropsOrphanedResults(t *testing.T) {
	ctx := context.Background()
	mem := New(3)

	msgs := append([]llm.Message{llm.UserMessage(llm.Text("go"))}, toolTurn("a")...)
	msgs = append(msgs, toolTurn("b")...)

	if err := mem.Save(ctx, msgs); err != nil {
		t.Fatal(err)
	}

	// The buffer holds [tool a, assistant b, tool b]; tool a lost its call.
	got, _ := mem.Retrieve(ctx, "")
	assertNoOrphans(t, got)

	if len(got) != 2 {
		t.Fatalf("got %d messages, want tool turn b only", len(got))
	}
}
