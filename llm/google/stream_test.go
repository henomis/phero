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
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/google"
)

func TestExecuteStream(t *testing.T) {
	f := newFake(t, strings.Join([]string{
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hmm.","thought":true}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[` +
			`{"functionCall":{"id":"c1","name":"f","args":{"k":"v"}},"thoughtSignature":"` + b64("sig-s") + `"}]},"finishReason":"STOP"}],` +
			`"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":4},"modelVersion":"gemini-test-002"}`,
	}, "\n"))

	var (
		text, reasoning string
		calls           []llm.ToolCall
		final           llm.StreamChunk
	)

	for chunk, err := range newClient(f).ExecuteStream(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))}) {
		if err != nil {
			t.Fatal(err)
		}

		text += chunk.TextDelta
		reasoning += chunk.ReasoningDelta

		if chunk.ToolCall != nil {
			calls = append(calls, *chunk.ToolCall)
		}

		if chunk.Done {
			final = chunk
		}
	}

	if text != "Hello" || reasoning != "Hmm." || len(calls) != 1 || calls[0].ID != "c1" {
		t.Errorf("deltas: text %q, reasoning %q, calls %+v", text, reasoning, calls)
	}

	if final.Message == nil || final.Message.TextContent() != "Hello" || final.Model != "gemini-test-002" ||
		final.Usage == nil || final.Usage.InputTokens != 9 || final.Usage.OutputTokens != 4 {
		t.Fatalf("terminal chunk = %+v", final)
	}

	if !strings.HasSuffix(f.paths[0], "models/gemini-test:streamGenerateContent") {
		t.Errorf("path = %s", f.paths[0])
	}

	// The streamed message round-trips its signature like a buffered one.
	history := []llm.Message{llm.UserMessage(llm.Text("q")), *final.Message, llm.ToolResultMessage("c1", llm.Text("r"))}
	if _, err := newClient(f).Execute(ctx, history); err != nil {
		t.Fatal(err)
	}

	if got := get(t, f.request(t, 1), "contents", 1, "parts", 1, "thoughtSignature"); got != b64("sig-s") {
		t.Errorf("signature sent back = %v", got)
	}
}

func TestExecuteStream_Error(t *testing.T) {
	f := newFake(t, `{"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}`)
	f.status = http.StatusServiceUnavailable

	var got error

	for _, err := range newClient(f).ExecuteStream(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))}) {
		if err != nil {
			got = err
		}
	}

	var pe *llm.ProviderError
	if !errors.As(got, &pe) || pe.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("err = %v", got)
	}
}

func TestExecuteStream_Blocked(t *testing.T) {
	f := newFake(t, `{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`)

	var got error

	for _, err := range newClient(f).ExecuteStream(ctx, []llm.Message{llm.UserMessage(llm.Text("q"))}) {
		if err != nil {
			got = err
		}
	}

	var e *google.NoContentError
	if !errors.As(got, &e) || e.BlockReason != "PROHIBITED_CONTENT" {
		t.Fatalf("err = %v", got)
	}
}
