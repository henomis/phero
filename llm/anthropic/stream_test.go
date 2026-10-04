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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/anthropic"
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

// A buffered request whose max_tokens implies more than ten minutes of
// generation is refused by the SDK before it reaches the wire. Execute must
// stream it instead and still return an ordinary assembled result, so callers
// can set the large budgets agentic and coding work needs.
func TestExecute_LargeMaxTokens_FallsBackToStreaming(t *testing.T) {
	frames := strings.Join([]string{
		sseEvent("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,"usage":{"input_tokens":7,"output_tokens":0}}}`),
		sseEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
		sseEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"long answer"}}`),
		sseEvent("content_block_stop", `{"type":"content_block_stop","index":0}`),
		sseEvent("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9}}`),
		sseEvent("message_stop", `{"type":"message_stop"}`),
	}, "")

	var body []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}

		body = b

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(frames))
	}))
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL), anthropic.WithMaxTokens(32000))

	res, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if !strings.Contains(string(body), `"stream":true`) {
		t.Errorf("request body = %s; want a streaming request", body)
	}

	if res.Message == nil || res.Message.TextContent() != "long answer" {
		t.Fatalf("message = %v, want the assembled text", res.Message)
	}

	if res.Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q, want claude-sonnet-4-6", res.Model)
	}

	if res.Usage == nil || res.Usage.InputTokens != 7 || res.Usage.OutputTokens != 9 {
		t.Errorf("usage = %+v, want in=7 out=9", res.Usage)
	}
}

// The fallback is for requests that cannot be buffered; an ordinary request
// must still be one plain call, since streaming costs an SSE round trip and
// changes nothing a caller of Execute can see.
func TestExecute_OrdinaryMaxTokens_StaysBuffered(t *testing.T) {
	var body []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}

		body = b

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x","type":"message","role":"assistant","model":"m",` +
			`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",` +
			`"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL), anthropic.WithMaxTokens(2048))

	if _, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if strings.Contains(string(body), `"stream":true`) {
		t.Errorf("request body = %s; want a buffered request", body)
	}
}
