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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/anthropic"
)

// -- helpers -----------------------------------------------------------------

// anthropicResponse mirrors the Anthropic Messages API response shape.
type anthropicResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []contentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        anthropicUsage `json:"usage"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// newTestServer returns an httptest.Server that serves the provided Anthropic
// response to all POST requests under any path.
func newTestServer(t *testing.T, resp anthropicResponse, statusCode int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

// -- constructor tests -------------------------------------------------------

func TestNew_NilAPIKey_DoesNotPanic(t *testing.T) {
	// Empty API key falls through to env-based lookup; we just check it doesn't panic.
	c := anthropic.New("")
	if c == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_WithModelOption(t *testing.T) {
	c := anthropic.New("key", anthropic.WithModel("claude-3-haiku-20240307"))
	if c == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_WithMaxTokensOption(t *testing.T) {
	c := anthropic.New("key", anthropic.WithMaxTokens(512))
	if c == nil {
		t.Fatal("New returned nil")
	}
}

// -- Execute tests -----------------------------------------------------------

func TestExecute_TextResponse(t *testing.T) {
	srv := newTestServer(t, anthropicResponse{
		ID:         "msg-test",
		Type:       "message",
		Role:       "assistant",
		Model:      anthropic.DefaultModel,
		StopReason: "end_turn",
		Content: []contentBlock{
			{Type: "text", Text: "Hello from Anthropic!"},
		},
		Usage: anthropicUsage{InputTokens: 12, OutputTokens: 6},
	}, http.StatusOK)
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "hi"}}

	result, err := c.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if result.Message == nil {
		t.Fatal("expected non-nil message")
	}
	if result.Message.Content != "Hello from Anthropic!" {
		t.Fatalf("expected %q, got %q", "Hello from Anthropic!", result.Message.Content)
	}
	if result.Usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if result.Usage.InputTokens != 12 || result.Usage.OutputTokens != 6 {
		t.Fatalf("usage mismatch: input=%d output=%d", result.Usage.InputTokens, result.Usage.OutputTokens)
	}
}

func TestExecute_WithToolUse(t *testing.T) {
	srv := newTestServer(t, anthropicResponse{
		ID:         "msg-tool",
		Type:       "message",
		Role:       "assistant",
		Model:      anthropic.DefaultModel,
		StopReason: "tool_use",
		Content: []contentBlock{
			{
				Type:  "tool_use",
				ID:    "tool-call-1",
				Name:  "get_weather",
				Input: json.RawMessage(`{"location":"Paris"}`),
			},
		},
		Usage: anthropicUsage{InputTokens: 25, OutputTokens: 10},
	}, http.StatusOK)
	defer srv.Close()

	type weatherInput struct {
		Location string `json:"location"`
	}
	tool, err := llm.NewTool("get_weather", "returns weather", func(_ context.Context, in *weatherInput) (string, error) {
		return "cloudy", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "weather in Paris?"}}

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
	if result.Message.ToolCalls[0].Function.Arguments != `{"location":"Paris"}` {
		t.Fatalf("unexpected arguments: %s", result.Message.ToolCalls[0].Function.Arguments)
	}
}

func TestExecute_SystemMessage_Converted(t *testing.T) {
	srv := newTestServer(t, anthropicResponse{
		ID:         "msg-sys",
		Type:       "message",
		Role:       "assistant",
		Model:      anthropic.DefaultModel,
		StopReason: "end_turn",
		Content:    []contentBlock{{Type: "text", Text: "ok"}},
		Usage:      anthropicUsage{InputTokens: 5, OutputTokens: 2},
	}, http.StatusOK)
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{
		{Role: llm.ChatMessageRoleSystem, Content: "You are a helpful assistant."},
		{Role: llm.ChatMessageRoleUser, Content: "hello"},
	}

	result, err := c.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if result.Message.Content != "ok" {
		t.Fatalf("expected %q, got %q", "ok", result.Message.Content)
	}
}

func TestExecute_UnsupportedRole_ReturnsError(t *testing.T) {
	// Provide a server so the client can be created, but the error should occur
	// before the network call due to message conversion.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{
		{Role: "banana", Content: "bad role"},
	}

	_, err := c.Execute(context.Background(), msgs, nil)
	if err == nil {
		t.Fatal("expected error for unsupported role")
	}
	var unsupported *anthropic.UnsupportedRoleError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected UnsupportedRoleError, got %T: %v", err, err)
	}
}

func TestExecute_ToolMessage_MissingToolCallID_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{
		{Role: llm.ChatMessageRoleUser, Content: "do it"},
		{
			Role:       llm.ChatMessageRoleTool,
			Content:    "result",
			ToolCallID: "", // missing — must error
		},
	}

	_, err := c.Execute(context.Background(), msgs, nil)
	if !errors.Is(err, anthropic.ErrToolMessageMissingToolCallID) {
		t.Fatalf("expected ErrToolMessageMissingToolCallID, got %v", err)
	}
}

func TestExecute_APIError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := anthropic.New("bad-key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{{Role: llm.ChatMessageRoleUser, Content: "hi"}}

	_, err := c.Execute(context.Background(), msgs, nil)
	if err == nil {
		t.Fatal("expected error from 401 response, got nil")
	}
}
