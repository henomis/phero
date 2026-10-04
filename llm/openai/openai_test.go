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
	"reflect"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/openai"
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
func newTestServer(t *testing.T, resp chatCompletionResponse) *httptest.Server {
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
	})
	defer srv.Close()

	c := openai.New("key", openai.WithModel("gpt-4o"), openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hello"))}

	result, err := c.Execute(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Message.TextContent() != "hi" {
		t.Fatalf("expected %q, got %q", "hi", result.Message.TextContent())
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
	})
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	result, err := c.Execute(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Message == nil {
		t.Fatal("expected non-nil message")
	}

	if result.Message.TextContent() != "Hello there!" {
		t.Fatalf("expected %q, got %q", "Hello there!", result.Message.TextContent())
	}

	if result.Usage == nil {
		t.Fatal("expected non-nil usage")
	}

	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 4 {
		t.Fatalf("usage mismatch: got input=%d output=%d", result.Usage.InputTokens, result.Usage.OutputTokens)
	}
}

func TestExecute_WithTemperature(t *testing.T) {
	var gotTemperature float64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close()
		}()

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		value, ok := payload["temperature"].(float64)
		if !ok {
			t.Fatalf("expected numeric temperature in request payload, got %T", payload["temperature"])
		}

		gotTemperature = value

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(chatCompletionResponse{
			Object: "chat.completion",
			ID:     "chatcmpl-temp",
			Model:  openai.DefaultModel,
			Choices: []choice{
				{Message: message{Role: "assistant", Content: "ok"}, Reason: "stop"},
			},
			Usage: usage{PromptTokens: 7, CompletionTokens: 2},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"), openai.WithTemperature(0.7))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	result, err := c.Execute(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if result.Message.TextContent() != "ok" {
		t.Fatalf("expected %q, got %q", "ok", result.Message.TextContent())
	}

	if gotTemperature != 0.7 {
		t.Fatalf("expected temperature 0.7, got %v", gotTemperature)
	}
}

func TestExecute_EmptyChoices_ReturnsError(t *testing.T) {
	srv := newTestServer(t, chatCompletionResponse{
		Object:  "chat.completion",
		ID:      "chatcmpl-2",
		Model:   openai.DefaultModel,
		Choices: []choice{},
	})
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	_, err := c.Execute(context.Background(), msgs)
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
	})
	defer srv.Close()

	type weatherInput struct {
		Location string `json:"location"`
	}

	tool, err := llm.NewTool("get_weather", "returns weather", func(_ context.Context, _ *weatherInput) (string, error) {
		return "sunny", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{llm.UserMessage(llm.Text("weather?"))}

	result, err := c.Execute(context.Background(), msgs, llm.WithTools(tool))
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
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	_, err := c.Execute(context.Background(), msgs)
	if err == nil {
		t.Fatal("expected error from 401 response, got nil")
	}
}

// payloadCapturingServer records the decoded JSON body of each request and
// replies with a minimal successful chat completion.
func payloadCapturingServer(t *testing.T, payload *map[string]any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			_ = r.Body.Close()
		}()

		var decoded map[string]any
		if err := json.NewDecoder(r.Body).Decode(&decoded); err != nil {
			t.Errorf("decode request: %v", err)
		}

		*payload = decoded

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(chatCompletionResponse{
			Object: "chat.completion",
			ID:     "chatcmpl-effort",
			Model:  openai.DefaultModel,
			Choices: []choice{
				{Message: message{Role: "assistant", Content: "ok"}, Reason: "stop"},
			},
			Usage: usage{PromptTokens: 7, CompletionTokens: 2},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
}

func TestExecute_WithReasoningEffort(t *testing.T) {
	var payload map[string]any

	srv := payloadCapturingServer(t, &payload)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"), openai.WithReasoningEffort("high"))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	if _, err := c.Execute(context.Background(), msgs); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if got, ok := payload["reasoning_effort"].(string); !ok || got != "high" {
		t.Fatalf("reasoning_effort = %v, want %q", payload["reasoning_effort"], "high")
	}
}

// TestExecute_NoReasoningEffort_OmitsField keeps unconfigured clients compatible
// with endpoints that reject the field.
func TestExecute_NoReasoningEffort_OmitsField(t *testing.T) {
	var payload map[string]any

	srv := payloadCapturingServer(t, &payload)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
	msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

	if _, err := c.Execute(context.Background(), msgs); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	if _, present := payload["reasoning_effort"]; present {
		t.Fatalf("reasoning_effort = %v, want omitted", payload["reasoning_effort"])
	}
}

func TestExecute_MaxTokens(t *testing.T) {
	tests := []struct {
		name        string
		opts        []openai.Option
		wantField   string
		wantAbsent  string
		wantValue   float64
		wantNoField bool
	}{
		{name: "unset", wantNoField: true},
		{name: "non-positive ignored", opts: []openai.Option{openai.WithMaxTokens(0), openai.WithLegacyMaxTokens(-1)}, wantNoField: true},
		{
			name:      "max_completion_tokens",
			opts:      []openai.Option{openai.WithMaxTokens(256)},
			wantField: "max_completion_tokens", wantAbsent: "max_tokens", wantValue: 256,
		},
		{
			name:      "legacy max_tokens",
			opts:      []openai.Option{openai.WithLegacyMaxTokens(128)},
			wantField: "max_tokens", wantAbsent: "max_completion_tokens", wantValue: 128,
		},
		{
			name:      "last option wins",
			opts:      []openai.Option{openai.WithLegacyMaxTokens(128), openai.WithMaxTokens(64)},
			wantField: "max_completion_tokens", wantAbsent: "max_tokens", wantValue: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload map[string]any

			srv := payloadCapturingServer(t, &payload)
			defer srv.Close()

			opts := append([]openai.Option{openai.WithBaseURL(srv.URL + "/v1")}, tt.opts...)
			c := openai.New("key", opts...)
			msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

			if _, err := c.Execute(context.Background(), msgs); err != nil {
				t.Fatalf("Execute: unexpected error: %v", err)
			}

			if tt.wantNoField {
				for _, f := range []string{"max_tokens", "max_completion_tokens"} {
					if _, present := payload[f]; present {
						t.Fatalf("%s = %v, want omitted", f, payload[f])
					}
				}

				return
			}

			if got, ok := payload[tt.wantField].(float64); !ok || got != tt.wantValue {
				t.Fatalf("%s = %v, want %v", tt.wantField, payload[tt.wantField], tt.wantValue)
			}

			if _, present := payload[tt.wantAbsent]; present {
				t.Fatalf("%s = %v, want omitted", tt.wantAbsent, payload[tt.wantAbsent])
			}
		})
	}
}

func TestExecute_ToolChoiceAndResponseFormat(t *testing.T) {
	tool, err := llm.NewTool("lookup", "look something up", func(_ context.Context, _ struct{}) (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	format, err := llm.NewResponseFormat[struct {
		Answer string `json:"answer"`
	}]("answer", "the final answer")
	if err != nil {
		t.Fatalf("NewResponseFormat: %v", err)
	}

	tests := []struct {
		name       string
		choice     llm.CallOption
		wantChoice any
	}{
		{name: "required", choice: llm.WithToolChoice(llm.ToolChoiceRequired), wantChoice: "required"},
		{name: "none", choice: llm.WithToolChoice(llm.ToolChoiceNone), wantChoice: "none"},
		{
			name:       "forced",
			choice:     llm.WithForcedTool("lookup"),
			wantChoice: map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload map[string]any

			srv := payloadCapturingServer(t, &payload)
			defer srv.Close()

			c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))
			msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

			if _, execErr := c.Execute(context.Background(), msgs,
				llm.WithTools(tool), tt.choice, llm.WithResponseFormat(format)); execErr != nil {
				t.Fatalf("Execute: %v", execErr)
			}

			if !reflect.DeepEqual(payload["tool_choice"], tt.wantChoice) {
				t.Fatalf("tool_choice = %#v, want %#v", payload["tool_choice"], tt.wantChoice)
			}

			rf, _ := payload["response_format"].(map[string]any)
			js, _ := rf["json_schema"].(map[string]any)

			if rf["type"] != "json_schema" || js["name"] != "answer" || js["strict"] != true ||
				js["description"] != "the final answer" {
				t.Fatalf("response_format = %#v", payload["response_format"])
			}

			if schema, _ := js["schema"].(map[string]any); schema["type"] != "object" {
				t.Fatalf("response_format schema = %#v, want an object schema", js["schema"])
			}
		})
	}
}

func TestExecute_InvalidCallOptions_FailBeforeRequest(t *testing.T) {
	var payload map[string]any

	srv := payloadCapturingServer(t, &payload)
	defer srv.Close()

	c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))

	_, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))},
		llm.WithToolChoice(llm.ToolChoiceRequired))
	if !errors.Is(err, llm.ErrInvalidToolChoice) {
		t.Fatalf("Execute() = %v, want ErrInvalidToolChoice", err)
	}

	if payload != nil {
		t.Fatal("request was sent despite invalid options")
	}
}

// TestExecute_ToolChoiceWithoutTools_IsOmitted verifies that auto or none on a
// call that offers no tools sends no tool_choice: OpenAI rejects tool_choice
// without tools, and with no tools both modes already hold.
func TestExecute_ToolChoiceWithoutTools_IsOmitted(t *testing.T) {
	for _, mode := range []llm.ToolChoiceMode{llm.ToolChoiceAuto, llm.ToolChoiceNone} {
		t.Run(string(mode), func(t *testing.T) {
			var payload map[string]any

			srv := payloadCapturingServer(t, &payload)
			defer srv.Close()

			c := openai.New("key", openai.WithBaseURL(srv.URL+"/v1"))

			if _, err := c.Execute(context.Background(), []llm.Message{llm.UserMessage(llm.Text("hi"))},
				llm.WithToolChoice(mode)); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			if _, present := payload["tool_choice"]; present {
				t.Fatalf("tool_choice = %#v, want omitted", payload["tool_choice"])
			}
		})
	}
}
