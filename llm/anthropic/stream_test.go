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

package anthropic_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/anthropic"
)

// sseEvent renders one SSE event with the given event name and JSON data.
func sseEvent(name, data string) string {
	return "event: " + name + "\ndata: " + data + "\n\n"
}

func TestExecuteStream_TextResponse(t *testing.T) {
	frames := strings.Join([]string{
		sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":0}}}`),
		sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`),
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" there"}}`),
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`),
		sseEvent("message_stop", `{"type":"message_stop"}`),
	}, "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(frames))
	}))
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))

	var (
		text  strings.Builder
		final llm.StreamChunk
	)
	for chunk, err := range c.ExecuteStream(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))}, nil) {
		if err != nil {
			t.Fatalf("ExecuteStream: %v", err)
		}
		text.WriteString(chunk.TextDelta)
		if chunk.Done {
			final = chunk
		}
	}

	if text.String() != "Hello there" {
		t.Fatalf("streamed text = %q, want %q", text.String(), "Hello there")
	}
	if final.Message == nil || final.Message.TextContent() != "Hello there" {
		t.Fatalf("final message = %v, want text %q", final.Message, "Hello there")
	}
	if final.Model != "claude-sonnet-4-6" {
		t.Fatalf("final model = %q, want claude-sonnet-4-6", final.Model)
	}
	if final.Usage == nil || final.Usage.InputTokens != 5 || final.Usage.OutputTokens != 3 {
		t.Fatalf("final usage = %+v, want in=5 out=3", final.Usage)
	}
}
