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

package memory

import (
"strings"
"testing"

"github.com/henomis/phero/llm"
)

func TestClampSummarySize(t *testing.T) {
	tests := []struct {
		name      string
		threshold uint
		size      uint
		want      uint
	}{
		{
			name:      "zero threshold returns size as-is",
			threshold: 0,
			size:      5,
			want:      5,
		},
		{
			name:      "zero size derives half of threshold",
			threshold: 10,
			size:      0,
			want:      5,
		},
		{
			name:      "zero size with odd threshold rounds down",
			threshold: 7,
			size:      0,
			want:      3,
		},
		{
			name:      "zero size with threshold=1 returns 1",
			threshold: 1,
			size:      0,
			want:      1,
		},
		{
			name:      "size equal to threshold is capped at threshold-1",
			threshold: 6,
			size:      6,
			want:      5,
		},
		{
			name:      "size greater than threshold is capped at threshold-1",
			threshold: 6,
			size:      10,
			want:      5,
		},
		{
			name:      "threshold=1 with size=1 is capped at 1",
			threshold: 1,
			size:      1,
			want:      1,
		},
		{
			name:      "normal: size strictly below threshold is unchanged",
			threshold: 10,
			size:      4,
			want:      4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
got := ClampSummarySize(tt.threshold, tt.size)
if got != tt.want {
t.Fatalf("ClampSummarySize(%d, %d) = %d, want %d", tt.threshold, tt.size, got, tt.want)
}
})
	}
}

func TestFormatSummaryPrompt(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.ChatMessageRoleUser, Content: "hello"},
		{Role: llm.ChatMessageRoleAssistant, Content: "world"},
	}

	result := FormatSummaryPrompt(msgs)

	if result.Role != llm.ChatMessageRoleUser {
		t.Fatalf("FormatSummaryPrompt role = %q, want %q", result.Role, llm.ChatMessageRoleUser)
	}
	if !strings.Contains(result.Content, "hello") {
		t.Fatalf("FormatSummaryPrompt content missing input text %q: %s", "hello", result.Content)
	}
	if !strings.Contains(result.Content, "world") {
		t.Fatalf("FormatSummaryPrompt content missing input text %q: %s", "world", result.Content)
	}
	if !strings.Contains(result.Content, llm.ChatMessageRoleUser) {
		t.Fatalf("FormatSummaryPrompt content missing role label %q: %s", llm.ChatMessageRoleUser, result.Content)
	}
	if !strings.Contains(result.Content, llm.ChatMessageRoleAssistant) {
		t.Fatalf("FormatSummaryPrompt content missing role label %q: %s", llm.ChatMessageRoleAssistant, result.Content)
	}
}

func TestFormatSummaryPrompt_EmptyConversation(t *testing.T) {
	result := FormatSummaryPrompt(nil)

	if result.Role != llm.ChatMessageRoleUser {
		t.Fatalf("FormatSummaryPrompt(nil) role = %q, want %q", result.Role, llm.ChatMessageRoleUser)
	}
	if result.Content == "" {
		t.Fatal("FormatSummaryPrompt(nil) returned empty content")
	}
}
