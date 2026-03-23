package simple

import (
	"context"
	"testing"

	"github.com/henomis/phero/llm"
)

type mockSummaryLLM struct {
	called int
}

func (m *mockSummaryLLM) Execute(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
	m.called++
	return &llm.Result{Message: &llm.Message{Role: llm.ChatMessageRoleSystem, Content: "summary"}}, nil
}

func TestMemorySave_SummarizationReplacesHistory(t *testing.T) {
	ctx := context.Background()

	mockLLM := &mockSummaryLLM{}
	mem := New(10, WithSummarization(mockLLM, 6, 4))

	msgs := []llm.Message{
		{Role: llm.ChatMessageRoleUser, Content: "hello1"},
		{Role: llm.ChatMessageRoleAssistant, Content: "hello2"},
		{Role: llm.ChatMessageRoleTool, Content: "hello3"},
		{Role: llm.ChatMessageRoleAssistant, Content: "hello4"},
		{Role: llm.ChatMessageRoleUser, Content: "hello5"},
		{Role: llm.ChatMessageRoleAssistant, Content: "hello6"},
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
		if got[i].Content != wantContents[i] {
			t.Fatalf("buffer[%d].Content: expected %q, got %q", i, wantContents[i], got[i].Content)
		}
	}
}
