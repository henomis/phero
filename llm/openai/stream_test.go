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

package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
)

// sseServer returns a server that writes the given SSE data frames followed by
// the terminating [DONE] sentinel.
func sseServer(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
		}

		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func TestExecuteStream_TextAndUsage(t *testing.T) {
	srv := sseServer(t,
		`{"id":"1","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
	)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL))

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

	if text.String() != "Hello" {
		t.Fatalf("streamed text = %q, want %q", text.String(), "Hello")
	}

	if final.Message == nil || final.Message.TextContent() != "Hello" {
		t.Fatalf("final message = %v, want text %q", final.Message, "Hello")
	}

	if final.Usage == nil || final.Usage.InputTokens != 5 || final.Usage.OutputTokens != 2 {
		t.Fatalf("final usage = %+v, want in=5 out=2", final.Usage)
	}

	if final.Model != "gpt-4o-mini" {
		t.Fatalf("final model = %q, want gpt-4o-mini", final.Model)
	}
}

func TestExecuteStream_AssemblesToolCall(t *testing.T) {
	srv := sseServer(t,
		`{"id":"1","object":"chat.completion.chunk","model":"gpt-4o-mini","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"ci"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Paris\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL))

	var (
		emittedCall *llm.ToolCall
		final       llm.StreamChunk
	)

	for chunk, err := range c.ExecuteStream(context.Background(), []llm.Message{llm.UserMessage(llm.Text("weather?"))}, nil) {
		if err != nil {
			t.Fatalf("ExecuteStream: %v", err)
		}

		if chunk.ToolCall != nil {
			emittedCall = chunk.ToolCall
		}

		if chunk.Done {
			final = chunk
		}
	}

	if emittedCall == nil {
		t.Fatal("expected a tool-call chunk")
	}

	if emittedCall.Function.Name != "get_weather" || emittedCall.Function.Arguments != `{"city":"Paris"}` {
		t.Fatalf("assembled tool call = %+v", emittedCall.Function)
	}

	if final.Message == nil || len(final.Message.ToolCalls) != 1 {
		t.Fatalf("final message tool calls = %v, want 1", final.Message)
	}
}
