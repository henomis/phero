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
	"io"
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
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// newTestServer returns an httptest.Server that serves the provided Anthropic
// response to all POST requests under any path.
func newTestServer(t *testing.T, resp anthropicResponse) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

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
	})
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	result, err := c.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Message == nil {
		t.Fatal("expected non-nil message")
	}

	if result.Message.TextContent() != "Hello from Anthropic!" {
		t.Fatalf("expected %q, got %q", "Hello from Anthropic!", result.Message.TextContent())
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
	})
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
	msgs := []llm.Message{llm.UserMessage(llm.Text("weather in Paris?"))}

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
	})
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{
		llm.SystemMessage("You are a helpful assistant."),
		llm.UserMessage(llm.Text("hello")),
	}

	result, err := c.Execute(context.Background(), msgs, nil)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Message.TextContent() != "ok" {
		t.Fatalf("expected %q, got %q", "ok", result.Message.TextContent())
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
		{Role: "banana", Parts: []llm.ContentPart{llm.Text("bad role")}},
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
		llm.UserMessage(llm.Text("do it")),
		llm.ToolResultMessage("", llm.Text("result")), // missing ToolCallID — must error
	}

	_, err := c.Execute(context.Background(), msgs, nil)
	if !errors.Is(err, anthropic.ErrToolMessageMissingToolCallID) {
		t.Fatalf("expected ErrToolMessageMissingToolCallID, got %v", err)
	}
}

// capturingServer records the raw request body of the last POST it receives and
// replies with a minimal valid assistant message.
func capturingServer(t *testing.T, body *[]byte) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}

		*body = b

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"x","type":"message","role":"assistant","model":"m",` +
			`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",` +
			`"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
}

// wireMessage is a minimal view of an Anthropic Messages API message used to
// assert the shape of the converted request payload.
type wireMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		IsError   *bool  `json:"is_error"`
	} `json:"content"`
}

type wireRequest struct {
	Messages []wireMessage `json:"messages"`
}

// TestExecute_ParallelToolResults_MergedIntoSingleUserMessage verifies that when
// the assistant issues parallel tool calls — and the framework appends one
// RoleTool message per call — the conversion groups all tool_result blocks into a
// single user turn, as Anthropic requires.
func TestExecute_ParallelToolResults_MergedIntoSingleUserMessage(t *testing.T) {
	var body []byte

	srv := capturingServer(t, &body)
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{
		llm.UserMessage(llm.Text("weather in Paris and London?")),
		llm.AssistantMessage(nil,
			llm.ToolCall{ID: "call_a", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: "w", Arguments: `{"city":"Paris"}`}},
			llm.ToolCall{ID: "call_b", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: "w", Arguments: `{"city":"London"}`}},
		),
		llm.ToolResultMessage("call_a", llm.Text("sunny")),
		llm.ToolResultMessage("call_b", llm.Text("rainy")),
	}

	if _, err := c.Execute(context.Background(), msgs, nil); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	var req wireRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}

	// Expect exactly 3 turns: user, assistant(tool_use x2), user(tool_result x2).
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", len(req.Messages), body)
	}

	last := req.Messages[2]
	if last.Role != "user" {
		t.Fatalf("expected final turn to be a user message, got %q", last.Role)
	}

	if len(last.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks in the final user message, got %d: %s", len(last.Content), body)
	}

	for i, want := range []string{"call_a", "call_b"} {
		if last.Content[i].Type != "tool_result" {
			t.Fatalf("block %d: expected tool_result, got %q", i, last.Content[i].Type)
		}

		if last.Content[i].ToolUseID != want {
			t.Fatalf("block %d: expected tool_use_id %q, got %q", i, want, last.Content[i].ToolUseID)
		}
	}
}

// TestExecute_ToolError_MapsToIsError verifies that a tool-result message
// flagged as an error sets is_error on the Anthropic tool_result block, while a
// successful result omits it.
func TestExecute_ToolError_MapsToIsError(t *testing.T) {
	var body []byte

	srv := capturingServer(t, &body)
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))

	errResult := llm.ToolResultMessage("call_err", llm.Text("boom"))
	errResult.ToolError = true

	msgs := []llm.Message{
		llm.UserMessage(llm.Text("do two things")),
		llm.AssistantMessage(nil,
			llm.ToolCall{ID: "call_ok", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: "t", Arguments: `{}`}},
			llm.ToolCall{ID: "call_err", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: "t", Arguments: `{}`}},
		),
		llm.ToolResultMessage("call_ok", llm.Text("fine")),
		errResult,
	}

	if _, err := c.Execute(context.Background(), msgs, nil); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	var req wireRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}

	last := req.Messages[len(req.Messages)-1]
	if len(last.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d: %s", len(last.Content), body)
	}

	byID := map[string]*bool{}
	for _, b := range last.Content {
		byID[b.ToolUseID] = b.IsError
	}

	if got := byID["call_ok"]; got != nil {
		t.Fatalf("expected is_error omitted for successful result, got %v", *got)
	}

	if got := byID["call_err"]; got == nil || !*got {
		t.Fatalf("expected is_error=true for failed result, got %v (body: %s)", got, body)
	}
}

// TestExecute_DefaultTemperature_MatchesAnthropicSpec verifies that an
// unconfigured client sends the Anthropic default temperature, and that
// WithTemperature overrides it.
func TestExecute_DefaultTemperature_MatchesAnthropicSpec(t *testing.T) {
	type wireTempRequest struct {
		Temperature *float64 `json:"temperature"`
	}

	t.Run("default", func(t *testing.T) {
		var body []byte

		srv := capturingServer(t, &body)
		defer srv.Close()

		c := anthropic.New("key", anthropic.WithBaseURL(srv.URL))
		if _, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))}, nil); err != nil {
			t.Fatalf("Execute: %v", err)
		}

		var req wireTempRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if req.Temperature == nil {
			t.Fatalf("expected temperature to be sent, body: %s", body)
		}

		if *req.Temperature != float64(anthropic.DefaultTemperature) {
			t.Fatalf("expected default temperature %v, got %v", anthropic.DefaultTemperature, *req.Temperature)
		}
	})

	t.Run("override", func(t *testing.T) {
		var body []byte

		srv := capturingServer(t, &body)
		defer srv.Close()

		c := anthropic.New("key", anthropic.WithBaseURL(srv.URL), anthropic.WithTemperature(0.2))
		if _, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))}, nil); err != nil {
			t.Fatalf("Execute: %v", err)
		}

		var req wireTempRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if req.Temperature == nil || *req.Temperature != float64(float32(0.2)) {
			t.Fatalf("expected temperature 0.2, got %v (body: %s)", req.Temperature, body)
		}
	})
}

func TestExecute_APIError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()

	c := anthropic.New("bad-key", anthropic.WithBaseURL(srv.URL))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	_, err := c.Execute(context.Background(), msgs, nil)
	if err == nil {
		t.Fatal("expected error from 401 response, got nil")
	}
}

// -- extended thinking & prompt caching --------------------------------------

func TestExecute_ThinkingResponse_ParsedAsReasoning(t *testing.T) {
	srv := newTestServer(t, anthropicResponse{
		ID:    "msg-think",
		Type:  "message",
		Role:  "assistant",
		Model: "claude-sonnet-4-6",
		Content: []contentBlock{
			{Type: "thinking", Thinking: "let me reason", Signature: "sig-123"},
			{Type: "text", Text: "the answer is 42"},
		},
		StopReason: "end_turn",
		Usage:      anthropicUsage{InputTokens: 5, OutputTokens: 7},
	})
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL), anthropic.WithThinking(1024))

	res, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("q"))}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := res.Message.ReasoningContent(); got != "let me reason" {
		t.Fatalf("ReasoningContent = %q, want %q", got, "let me reason")
	}

	if got := res.Message.TextContent(); got != "the answer is 42" {
		t.Fatalf("TextContent = %q, want %q (reasoning must not leak into text)", got, "the answer is 42")
	}

	var sig string

	for _, p := range res.Message.Parts {
		if p.Type == llm.ContentTypeReasoning {
			sig = p.Signature
		}
	}

	if sig != "sig-123" {
		t.Fatalf("reasoning signature = %q, want %q", sig, "sig-123")
	}
}

func TestExecute_WithThinking_SetsConfigAndOmitsTemperature(t *testing.T) {
	var body []byte

	srv := capturingServer(t, &body)
	defer srv.Close()

	c := anthropic.New("key",
		anthropic.WithBaseURL(srv.URL),
		anthropic.WithMaxTokens(512),
		anthropic.WithThinking(2048),
	)
	if _, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var req struct {
		MaxTokens   int64    `json:"max_tokens"`
		Temperature *float64 `json:"temperature"`
		Thinking    *struct {
			Type         string `json:"type"`
			BudgetTokens int64  `json:"budget_tokens"`
		} `json:"thinking"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 2048 {
		t.Fatalf("thinking config = %+v, want enabled/2048", req.Thinking)
	}

	if req.Temperature != nil {
		t.Fatalf("temperature = %v, want omitted under extended thinking", *req.Temperature)
	}
	// max_tokens (512) must be raised above the budget (2048).
	if req.MaxTokens <= 2048 {
		t.Fatalf("max_tokens = %d, want > 2048 (budget headroom)", req.MaxTokens)
	}
}

func TestExecute_ReasoningRoundTrip_EmitsThinkingBlockFirst(t *testing.T) {
	var body []byte

	srv := capturingServer(t, &body)
	defer srv.Close()

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL), anthropic.WithThinking(1024))

	msgs := []llm.Message{
		llm.UserMessage(llm.Text("q")),
		llm.AssistantMessage([]llm.ContentPart{
			llm.Reasoning("prior thought", "sig-xyz"),
			llm.Text("prior answer"),
		}),
		llm.UserMessage(llm.Text("follow up")),
	}
	if _, err := c.Execute(context.Background(), msgs, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string `json:"type"`
				Signature string `json:"signature"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	var assistant *struct {
		Role    string `json:"role"`
		Content []struct {
			Type      string `json:"type"`
			Signature string `json:"signature"`
		} `json:"content"`
	}
	for i := range req.Messages {
		if req.Messages[i].Role == "assistant" {
			assistant = &req.Messages[i]
			break
		}
	}

	if assistant == nil || len(assistant.Content) == 0 {
		t.Fatalf("no assistant message with content: %s", body)
	}

	if assistant.Content[0].Type != "thinking" {
		t.Fatalf("first assistant block = %q, want thinking: %s", assistant.Content[0].Type, body)
	}

	if assistant.Content[0].Signature != "sig-xyz" {
		t.Fatalf("thinking signature = %q, want sig-xyz", assistant.Content[0].Signature)
	}
}

func TestExecute_WithPromptCaching_MarksSystemAndLastTool(t *testing.T) {
	var body []byte

	srv := capturingServer(t, &body)
	defer srv.Close()

	tool, err := llm.NewTool("t", "desc", func(_ context.Context, _ *struct{}) (string, error) { return "", nil })
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	c := anthropic.New("key", anthropic.WithBaseURL(srv.URL), anthropic.WithPromptCaching())

	msgs := []llm.Message{
		llm.SystemMessage("you are helpful"),
		llm.UserMessage(llm.Text("hi")),
	}
	if _, err := c.Execute(context.Background(), msgs, []*llm.Tool{tool}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var req struct {
		System []struct {
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"system"`
		Tools []struct {
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	if len(req.System) == 0 || req.System[len(req.System)-1].CacheControl == nil {
		t.Fatalf("expected cache_control on system block: %s", body)
	}

	if len(req.Tools) == 0 || req.Tools[len(req.Tools)-1].CacheControl == nil {
		t.Fatalf("expected cache_control on last tool: %s", body)
	}
}
