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
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/openai"
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
		`{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7,`+
			`"prompt_tokens_details":{"cached_tokens":1},"completion_tokens_details":{"reasoning_tokens":1}}}`,
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

	want := llm.Usage{InputTokens: 4, OutputTokens: 2, CacheReadTokens: 1, ReasoningTokens: 1}
	if final.Usage == nil || *final.Usage != want {
		t.Fatalf("final usage = %+v, want %+v", final.Usage, want)
	}

	if final.FinishReason != llm.FinishStop || final.RawFinishReason != "stop" {
		t.Fatalf("finish = %q (%q), want stop", final.FinishReason, final.RawFinishReason)
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

// TestExecuteStream_ForwardsReasoningEffort verifies the streaming path sends the
// same reasoning_effort field as the buffered one.
func TestExecuteStream_ForwardsReasoningEffort(t *testing.T) {
	var payload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}

		if unmarshalErr := json.Unmarshal(body, &payload); unmarshalErr != nil {
			t.Errorf("unmarshal request: %v", unmarshalErr)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"ok"},` +
			`"finish_reason":"stop"}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"), openai.WithReasoningEffort("low"))

	for _, err := range c.ExecuteStream(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))}, nil) {
		if err != nil {
			t.Fatalf("ExecuteStream: %v", err)
		}
	}

	if got, ok := payload["reasoning_effort"].(string); !ok || got != "low" {
		t.Fatalf("reasoning_effort = %v, want %q", payload["reasoning_effort"], "low")
	}
}

func TestExecuteStream_KeepsReasoningInFinalMessage(t *testing.T) {
	srv := sseServer(t,
		`{"id":"1","object":"chat.completion.chunk","model":"qwen","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Think"}}]}`,
		`{"choices":[{"index":0,"delta":{"reasoning_content":"ing."}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Answer"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL))

	var final llm.StreamChunk

	for chunk, err := range c.ExecuteStream(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))}, nil) {
		if err != nil {
			t.Fatalf("ExecuteStream: %v", err)
		}

		if chunk.Done {
			final = chunk
		}
	}

	if final.Message == nil || final.Message.ReasoningContent() != "Thinking." || final.Message.TextContent() != "Answer" {
		t.Fatalf("final message = %+v, want reasoning and text", final.Message)
	}

	if final.Message.Parts[0].Type != llm.ContentTypeReasoning {
		t.Fatalf("parts = %+v, want the reasoning first", final.Message.Parts)
	}
}
