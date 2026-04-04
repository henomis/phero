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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
)

// -- helpers -----------------------------------------------------------------

// chatCompletionResponse mirrors the OpenAI Chat Completions response shape
// just enough to satisfy the go-openai JSON decoder.
type chatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Index   int     `json:"index"`
	Message message `json:"message"`
	Reason  string  `json:"finish_reason"`
}

type message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
}

type toolCall struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Function function `json:"function"`
}

type function struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// newTestServer returns an httptest.Server that serves the provided
// completion response for all POST requests to /v1/chat/completions.
func newTestServer(t *testing.T, resp chatCompletionResponse, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

// -- constructor tests -------------------------------------------------------

func TestNew_DefaultModel(t *testing.T) {
	c := openai.New("key")
	if c == nil {
		t.Fatal("New returned nil")
	}
}

func TestWithModel_ChangesModel(t *testing.T) {
	srv := newTestServer(t, chatCompletionResponse{
		Object: "chat.completion",
		ID:     "chatcmpl-test",
		Model:  "gpt-4o",
		Choices: []choice{
			{Message: message{Role: "assistant", Content: "hi"}, Reason: "stop"},
		},
		Usage: usage{PromptTokens: 5, CompletionTokens: 3},
	}, http.StatusOK)
	defer srv.Close()

	c := openai.New("key", openai.WithModel("gpt-4o"), openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "hello"}}

	result, err := c.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if result.Message.Content != "hi" {
		t.Fatalf("expected %q, got %q", "hi", result.Message.Content)
	}
}

// -- Execute tests -----------------------------------------------------------

func TestExecute_TextResponse(t *testing.T) {
	srv := newTestServer(t, chatCompletionResponse{
		Object: "chat.completion",
		ID:     "chatcmpl-1",
		Model:  openai.DefaultModel,
		Choices: []choice{
			{Message: message{Role: "assistant", Content: "Hello there!"}, Reason: "stop"},
		},
		Usage: usage{PromptTokens: 10, CompletionTokens: 4},
	}, http.StatusOK)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "hi"}}

	result, err := c.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if result.Message == nil {
		t.Fatal("expected non-nil message")
	}
	if result.Message.Content != "Hello there!" {
		t.Fatalf("expected %q, got %q", "Hello there!", result.Message.Content)
	}
	if result.Usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 4 {
		t.Fatalf("usage mismatch: got input=%d output=%d", result.Usage.InputTokens, result.Usage.OutputTokens)
	}
}

func TestExecute_EmptyChoices_ReturnsError(t *testing.T) {
	srv := newTestServer(t, chatCompletionResponse{
		Object:  "chat.completion",
		ID:      "chatcmpl-2",
		Model:   openai.DefaultModel,
		Choices: []choice{},
	}, http.StatusOK)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "hi"}}

	_, err := c.Execute(context.Background(), msgs, nil)
	if !errors.Is(err, openai.ErrEmptyResponse) {
		t.Fatalf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestExecute_WithToolCalls(t *testing.T) {
	srv := newTestServer(t, chatCompletionResponse{
		Object: "chat.completion",
		ID:     "chatcmpl-3",
		Model:  openai.DefaultModel,
		Choices: []choice{
			{
				Message: message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []toolCall{
						{
							ID:   "call-abc",
							Type: "function",
							Function: function{
								Name:      "get_weather",
								Arguments: `{"location":"London"}`,
							},
						},
					},
				},
				Reason: "tool_calls",
			},
		},
		Usage: usage{PromptTokens: 20, CompletionTokens: 8},
	}, http.StatusOK)
	defer srv.Close()

	type weatherInput struct {
		Location string `json:"location"`
	}
	tool, err := llm.NewTool("get_weather", "returns weather", func(_ context.Context, in *weatherInput) (string, error) {
		return "sunny", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "weather?"}}

	result, err := c.Execute(context.Background(), msgs, []*llm.Tool{tool})
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(result.Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.Message.ToolCalls))
	}
	if result.Message.ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("expected tool %q, got %q", "get_weather", result.Message.ToolCalls[0].Function.Name)
	}
}

func TestExecute_APIError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"invalid_request_error"}}`))
	}))
	defer srv.Close()

	c := openai.New("bad-key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "hi"}}

	_, err := c.Execute(context.Background(), msgs, nil)
	if err == nil {
		t.Fatal("expected error from 401 response, got nil")
	}
}
