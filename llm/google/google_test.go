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

package google_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/google"
)

// -- fake Gemini API -----------------------------------------------------------

// fakeGemini answers generateContent and streamGenerateContent requests from a
// script, one answer per request, and records the request bodies.
type fakeGemini struct {
	*httptest.Server

	mu       sync.Mutex
	answers  []string // JSON responses; a stream answer is several, joined by "\n"
	status   int      // when not 0, every answer is this status with answers[0] as body
	requests []map[string]any
	paths    []string
	keys     []string
}

func newFake(t *testing.T, answers ...string) *fakeGemini {
	t.Helper()

	f := &fakeGemini{answers: answers}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)

	return f
}

func (f *fakeGemini) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	body, _ := io.ReadAll(r.Body)

	var req map[string]any

	_ = json.Unmarshal(body, &req)

	f.requests = append(f.requests, req)
	f.paths = append(f.paths, r.URL.Path)
	f.keys = append(f.keys, r.Header.Get("x-goog-api-key"))

	if f.status != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.answers[0])

		return
	}

	answer := `{"candidates":[{"content":{"role":"model","parts":[{"text":"default"}]}}]}`
	if len(f.answers) > 0 {
		answer, f.answers = f.answers[0], f.answers[1:]
	}

	if strings.HasSuffix(r.URL.Path, ":streamGenerateContent") {
		w.Header().Set("Content-Type", "text/event-stream")

		for chunk := range strings.SplitSeq(answer, "\n") {
			_, _ = fmt.Fprintf(w, "data: %s\r\n\r\n", chunk)
		}

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, answer)
}

// request returns the i-th recorded request body.
func (f *fakeGemini) request(t *testing.T, i int) map[string]any {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	if i >= len(f.requests) {
		t.Fatalf("request %d not received (%d)", i, len(f.requests))
	}

	return f.requests[i]
}

func (f *fakeGemini) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.requests)
}

func newClient(f *fakeGemini, opts ...google.Option) *google.Client {
	return google.New("test-key", append([]google.Option{google.WithBaseURL(f.URL), google.WithModel("gemini-test")}, opts...)...)
}

// get walks a decoded JSON document: a string is a key, an int an index.
func get(t *testing.T, v any, path ...any) any {
	t.Helper()

	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("at %v: not an object: %v", path, v)
			}

			v = m[k]
		case int:
			s, ok := v.([]any)
			if !ok || k >= len(s) {
				t.Fatalf("at %v: no index %d in %v", path, k, v)
			}

			v = s[k]
		}
	}

	return v
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

var ctx = context.Background()

// -- Execute -------------------------------------------------------------------

func TestExecute_TextAndUsage(t *testing.T) {
	f := newFake(t, `{
		"candidates":[{"content":{"role":"model","parts":[{"text":"Hello "},{"text":"there"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":20,"cachedContentTokenCount":5,"candidatesTokenCount":7,"thoughtsTokenCount":3},
		"modelVersion":"gemini-test-001"}`)

	res, err := newClient(f).Execute(ctx, []llm.Message{
		llm.SystemMessage("Be brief."),
		llm.SystemMessage("Be kind."),
		llm.UserMessage(llm.Text("hi")),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := res.Message.TextContent(); got != "Hello there" {
		t.Errorf("text = %q", got)
	}

	if res.Model != "gemini-test-001" {
		t.Errorf("model = %q", res.Model)
	}

	want := llm.Usage{InputTokens: 15, OutputTokens: 10, CacheReadTokens: 5}
	if res.Usage == nil || *res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}

	req := f.request(t, 0)
	if got := get(t, req, "systemInstruction", "parts", 0, "text"); got != "Be brief.\n\nBe kind." {
		t.Errorf("system = %v", got)
	}

	if get(t, req, "contents", 0, "role") != "user" || get(t, req, "contents", 0, "parts", 0, "text") != "hi" {
		t.Errorf("contents = %v", req["contents"])
	}

	if !strings.HasSuffix(f.paths[0], "models/gemini-test:generateContent") || f.keys[0] != "test-key" {
		t.Errorf("path = %s, key = %q", f.paths[0], f.keys[0])
	}

	// No option set: the model's own defaults apply.
	if gc, ok := req["generationConfig"].(map[string]any); ok && (gc["temperature"] != nil || gc["thinkingConfig"] != nil) {
		t.Errorf("generationConfig = %v", gc)
	}
}

// The case Gemini 3 is strict about: the signature of a function call must
// come back on that call when the conversation continues.
func TestExecute_CallSignatureRoundTrip(t *testing.T) {
	f := newFake(t,
		`{"candidates":[{"content":{"role":"model","parts":[
			{"functionCall":{"id":"c1","name":"energy_summary","args":{"date":"today"}},"thoughtSignature":"`+b64("sig-1")+`"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"66% charged."}]}}]}`)
	client := newClient(f)

	history := make([]llm.Message, 0, 3) //nolint:mnd // question, call, result
	history = append(history, llm.UserMessage(llm.Text("How is the battery?")))

	res, err := client.Execute(ctx, history)
	if err != nil {
		t.Fatal(err)
	}

	msg := res.Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", msg.ToolCalls)
	}

	tc := msg.ToolCalls[0]
	if tc.ID != "c1" || tc.Function.Name != "energy_summary" || tc.Function.Arguments != `{"date":"today"}` {
		t.Errorf("tool call = %+v", tc)
	}

	if msg.ReasoningContent() != "" || msg.TextContent() != "" {
		t.Errorf("a signature must not show as text or reasoning: %+v", msg.Parts)
	}

	history = append(history, *msg, llm.ToolResultMessage("c1", llm.Text(`{"soc":66}`)))

	res, err = client.Execute(ctx, history)
	if err != nil {
		t.Fatal(err)
	}

	if res.Message.TextContent() != "66% charged." {
		t.Errorf("final = %q", res.Message.TextContent())
	}

	req := f.request(t, 1)

	call := get(t, req, "contents", 1, "parts", 0)
	if get(t, call, "thoughtSignature") != b64("sig-1") || get(t, call, "functionCall", "id") != "c1" ||
		get(t, call, "functionCall", "args", "date") != "today" || get(t, req, "contents", 1, "role") != "model" {
		t.Errorf("model turn = %v", call)
	}

	resp := get(t, req, "contents", 2, "parts", 0, "functionResponse")
	if get(t, resp, "id") != "c1" || get(t, resp, "name") != "energy_summary" ||
		get(t, resp, "response", "output") != `{"soc":66}` || get(t, req, "contents", 2, "role") != "user" {
		t.Errorf("function response = %v", resp)
	}
}

// Parallel calls without ids: the client makes ids up for phero, never sends
// them, keeps the one signature on the first call, and merges the results.
func TestExecute_ParallelCallsWithoutIDs(t *testing.T) {
	f := newFake(t,
		`{"candidates":[{"content":{"role":"model","parts":[
			{"functionCall":{"name":"a","args":{}},"thoughtSignature":"`+b64("sig-a")+`"},
			{"functionCall":{"name":"b","args":{"x":1}}}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]}}]}`)
	client := newClient(f)

	history := make([]llm.Message, 0, 4) //nolint:mnd // question, calls, two results
	history = append(history, llm.UserMessage(llm.Text("go")))

	res, err := client.Execute(ctx, history)
	if err != nil {
		t.Fatal(err)
	}

	calls := res.Message.ToolCalls
	if len(calls) != 2 || calls[0].ID == "" || calls[0].ID == calls[1].ID || calls[0].Function.Arguments != "{}" {
		t.Fatalf("tool calls = %+v", calls)
	}

	history = append(history, *res.Message,
		llm.ToolResultMessage(calls[0].ID, llm.Text("A")),
		llm.Message{Role: llm.RoleTool, ToolCallID: calls[1].ID, Parts: []llm.ContentPart{llm.Text("boom")}, ToolError: true})

	if _, err = client.Execute(ctx, history); err != nil {
		t.Fatal(err)
	}

	req := f.request(t, 1)

	model := get(t, req, "contents", 1, "parts").([]any)
	if len(model) != 2 || get(t, model, 0, "thoughtSignature") != b64("sig-a") || get(t, model, 1, "thoughtSignature") != nil {
		t.Errorf("model turn = %v", model)
	}

	for i := range 2 {
		if id := get(t, model, i, "functionCall", "id"); id != nil {
			t.Errorf("made-up id sent back: %v", id)
		}
	}

	if contents := get(t, req, "contents").([]any); len(contents) != 3 {
		t.Fatalf("the two results must be one turn: %v", contents)
	}

	results := get(t, req, "contents", 2, "parts").([]any)
	if get(t, results, 0, "functionResponse", "name") != "a" || get(t, results, 0, "functionResponse", "response", "output") != "A" ||
		get(t, results, 1, "functionResponse", "name") != "b" || get(t, results, 1, "functionResponse", "response", "error") != "boom" {
		t.Errorf("results = %v", results)
	}
}

func TestExecute_ThoughtsAndTextSignature(t *testing.T) {
	f := newFake(t,
		`{"candidates":[{"content":{"role":"model","parts":[
			{"text":"Let me think.","thought":true},
			{"text":"Answer.","thoughtSignature":"`+b64("sig-text")+`"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"again"}]}}]}`)
	client := newClient(f, google.WithIncludeThoughts())

	res, err := client.Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))})
	if err != nil {
		t.Fatal(err)
	}

	if res.Message.ReasoningContent() != "Let me think." || res.Message.TextContent() != "Answer." {
		t.Errorf("message = %+v", res.Message.Parts)
	}

	_, err = client.Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("q")), *res.Message, llm.UserMessage(llm.Text("more"))})
	if err != nil {
		t.Fatal(err)
	}

	req := f.request(t, 1)

	model := get(t, req, "contents", 1, "parts").([]any)
	if len(model) != 1 || get(t, model, 0, "text") != "Answer." || get(t, model, 0, "thoughtSignature") != b64("sig-text") {
		t.Errorf("model turn = %v (a thought without signature is not sent back)", model)
	}

	if get(t, req, "generationConfig", "thinkingConfig", "includeThoughts") != true {
		t.Errorf("generationConfig = %v", req["generationConfig"])
	}
}

func TestExecute_CallOptions(t *testing.T) {
	type input struct {
		City string `json:"city" jsonschema:"description=City name"`
	}

	tool, err := llm.NewTool("weather", "Weather of a city.", func(context.Context, input) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}

	format, err := llm.NewResponseFormat[input]("place", "A place.")
	if err != nil {
		t.Fatal(err)
	}

	f := newFake(t)
	client := newClient(f, google.WithTemperature(0.2), google.WithMaxTokens(256), google.WithThinkingLevel(google.ThinkingLow))

	if _, err = client.Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))},
		llm.WithTools(tool), llm.WithForcedTool("weather"), llm.WithResponseFormat(format)); err != nil {
		t.Fatal(err)
	}

	req := f.request(t, 0)

	decl := get(t, req, "tools", 0, "functionDeclarations", 0)
	if get(t, decl, "name") != "weather" || get(t, decl, "description") != "Weather of a city." ||
		get(t, decl, "parametersJsonSchema", "properties", "city", "type") != "string" {
		t.Errorf("declaration = %v", decl)
	}

	fcc := get(t, req, "toolConfig", "functionCallingConfig")
	if get(t, fcc, "mode") != "ANY" || get(t, fcc, "allowedFunctionNames", 0) != "weather" {
		t.Errorf("functionCallingConfig = %v", fcc)
	}

	gc := get(t, req, "generationConfig")
	if get(t, gc, "responseMimeType") != "application/json" || get(t, gc, "responseJsonSchema", "description") != "A place." ||
		get(t, gc, "temperature") != 0.2 || get(t, gc, "maxOutputTokens") != 256.0 || get(t, gc, "thinkingConfig", "thinkingLevel") != "LOW" {
		t.Errorf("generationConfig = %v", gc)
	}

	for mode, want := range map[llm.ToolChoiceMode]string{llm.ToolChoiceAuto: "AUTO", llm.ToolChoiceNone: "NONE", llm.ToolChoiceRequired: "ANY"} {
		if _, err = client.Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))}, llm.WithTools(tool), llm.WithToolChoice(mode)); err != nil {
			t.Fatal(err)
		}

		if got := get(t, f.request(t, f.count()-1), "toolConfig", "functionCallingConfig", "mode"); got != want {
			t.Errorf("%s: mode = %v, want %s", mode, got, want)
		}
	}
}

func TestExecute_InvalidInputFailsBeforeRequest(t *testing.T) {
	f := newFake(t)
	client := newClient(f)
	user := llm.UserMessage(llm.Text("q"))

	tests := []struct {
		name     string
		messages []llm.Message
		opts     []llm.CallOption
		check    func(error) bool
	}{
		{
			"forced tool not offered",
			[]llm.Message{user},
			[]llm.CallOption{llm.WithForcedTool("nope")},
			func(err error) bool { return errors.Is(err, llm.ErrInvalidToolChoice) },
		},
		{
			"unsupported role",
			[]llm.Message{{Role: "critic"}},
			nil,
			func(err error) bool { var e *google.UnsupportedRoleError; return errors.As(err, &e) },
		},
		{
			"tool result without id",
			[]llm.Message{user, llm.ToolResultMessage("", llm.Text("x"))},
			nil,
			func(err error) bool { return errors.Is(err, google.ErrToolMessageMissingToolCallID) },
		},
		{
			"tool result answering nothing",
			[]llm.Message{user, llm.ToolResultMessage("ghost", llm.Text("x"))},
			nil,
			func(err error) bool { var e *google.UnknownToolCallError; return errors.As(err, &e) },
		},
		{
			"image url",
			[]llm.Message{llm.UserMessage(llm.ImageURL("https://example.com/a.png"))},
			nil,
			func(err error) bool { return errors.Is(err, google.ErrImageURLUnsupported) },
		},
		{
			"invalid tool arguments",
			[]llm.Message{user, llm.AssistantMessage(nil, llm.ToolCall{ID: "c", Function: llm.FunctionCall{Name: "f", Arguments: "{"}})},
			nil,
			func(err error) bool { var e *google.ToolArgumentsParseError; return errors.As(err, &e) },
		},
	}

	for _, tt := range tests {
		if _, err := client.Execute(ctx, tt.messages, tt.opts...); !tt.check(err) {
			t.Errorf("%s: err = %v", tt.name, err)
		}
	}

	if n := f.count(); n != 0 {
		t.Errorf("%d requests sent", n)
	}
}

func TestExecute_InlineImage(t *testing.T) {
	f := newFake(t)

	if _, err := newClient(f).Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("what is it?"), llm.ImageBase64("image/png", b64("png-bytes")))}); err != nil {
		t.Fatal(err)
	}

	blob := get(t, f.request(t, 0), "contents", 0, "parts", 1, "inlineData")
	if get(t, blob, "mimeType") != "image/png" || get(t, blob, "data") != b64("png-bytes") {
		t.Errorf("inlineData = %v", blob)
	}
}

func TestExecute_APIError(t *testing.T) {
	f := newFake(t, `{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED",
		"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"7s"}]}}`)
	f.status = http.StatusTooManyRequests

	_, err := newClient(f).Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))})

	var pe *llm.ProviderError
	if !errors.As(err, &pe) || pe.Provider != "google" || pe.StatusCode != 429 || pe.RetryAfter != 7*time.Second {
		t.Fatalf("err = %#v", err)
	}
}

func TestExecute_Blocked(t *testing.T) {
	f := newFake(t, `{"promptFeedback":{"blockReason":"SAFETY"}}`)

	_, err := newClient(f).Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))})

	var e *google.NoContentError
	if !errors.As(err, &e) || e.BlockReason != "SAFETY" {
		t.Fatalf("err = %v", err)
	}
}

func TestNew_WithoutKey(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	_, err := google.New("").Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))})

	var e *google.ClientInitError
	if !errors.As(err, &e) {
		t.Fatalf("err = %v", err)
	}
}
