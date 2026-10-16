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

//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	llmgoogle "github.com/henomis/phero/v2/llm/google"
	"github.com/henomis/phero/v2/llm/middleware"
)

// The Google tests call the real Gemini API: they need GEMINI_API_KEY (or
// GOOGLE_API_KEY) and skip without it. GOOGLE_MODEL picks the model.
const defaultGoogleModel = "gemini-3.5-flash-lite"

func buildGoogleLLM(t *testing.T) *llmgoogle.Client {
	t.Helper()

	key := envOr("GEMINI_API_KEY", envOr("GOOGLE_API_KEY", ""))
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}

	return llmgoogle.New(key, llmgoogle.WithModel(envOr("GOOGLE_MODEL", defaultGoogleModel)))
}

// overloaded reports a rate limit or a server-side error: Gemini's load, not
// the provider's behavior.
func overloaded(err error) bool {
	var pe *llm.ProviderError

	return errors.As(err, &pe) && (pe.StatusCode == http.StatusTooManyRequests || pe.StatusCode >= http.StatusInternalServerError)
}

// buildRetryingGoogleLLM retries buffered calls while Gemini is overloaded.
// Streaming tests use buildGoogleLLM, which the middleware would turn into a
// buffered client.
func buildRetryingGoogleLLM(t *testing.T) llm.LLM {
	t.Helper()

	retry, err := middleware.NewRetry(4, middleware.WithInitialBackoff(2*time.Second), middleware.WithShouldRetry(overloaded))
	if err != nil {
		t.Fatal(err)
	}

	return llm.Use(buildGoogleLLM(t), retry)
}

// skipIfOverloaded skips a test that failed only because Gemini was busy.
func skipIfOverloaded(t *testing.T, err error) {
	t.Helper()

	if overloaded(err) {
		t.Skipf("Gemini overloaded: %v", err)
	}
}

// travelAgent needs two sequential tool calls: where a person lives, then the
// weather there. Gemini 3 rejects the second request unless the first call's
// thought signature comes back, so the run passing proves the round trip.
func travelAgent(t *testing.T, client llm.LLM) (*agent.Agent, *atomic.Int32) {
	t.Helper()

	type personInput struct {
		Person string `json:"person" jsonschema:"description=The person's name"`
	}

	type cityInput struct {
		City string `json:"city" jsonschema:"description=City name"`
	}

	calls := &atomic.Int32{}

	whereTool, err := llm.NewTool("where_lives", "Returns the city a person lives in.", func(_ context.Context, in *personInput) (string, error) {
		calls.Add(1)
		return "Zorbania City", nil
	})
	if err != nil {
		t.Fatal(err)
	}

	weatherTool, err := llm.NewTool("weather", "Returns today's weather in a city.", func(_ context.Context, in *cityInput) (string, error) {
		calls.Add(1)
		if strings.Contains(strings.ToLower(in.City), "zorbania") {
			return "purple snow, -7 degrees", nil
		}

		return "unknown city", nil
	})
	if err != nil {
		t.Fatal(err)
	}

	a, err := agent.New(client, "travel", "Answer with the tools only. First find where the person lives, then the weather there.")
	if err != nil {
		t.Fatal(err)
	}

	for _, tool := range []*llm.Tool{whereTool, weatherTool} {
		if err := a.AddTool(tool); err != nil {
			t.Fatal(err)
		}
	}

	return a, calls
}

func TestGoogleLLM_TextGeneration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := buildRetryingGoogleLLM(t).Execute(ctx, []llm.Message{
		llm.SystemMessage("Answer with one word."),
		llm.UserMessage(llm.Text("What is the capital of France?")),
	})
	if err != nil {
		skipIfOverloaded(t, err)
		t.Fatal(err)
	}

	if !strings.Contains(strings.ToLower(res.Message.TextContent()), "paris") {
		t.Errorf("answer = %q", res.Message.TextContent())
	}

	if res.Usage == nil || res.Usage.InputTokens == 0 || res.Usage.OutputTokens == 0 {
		t.Errorf("usage = %+v", res.Usage)
	}
}

func TestGoogleAgent_SequentialToolCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	a, calls := travelAgent(t, buildRetryingGoogleLLM(t))

	res, err := a.Run(ctx, llm.Text("What is the weather like where Ada lives?"))
	if err != nil {
		skipIfOverloaded(t, err)
		t.Fatalf("Run: %v", err)
	}

	if calls.Load() < 2 || !strings.Contains(strings.ToLower(res.TextContent()), "purple snow") {
		t.Errorf("calls = %d, answer = %q", calls.Load(), res.TextContent())
	}
}

func TestGoogleAgent_SequentialToolCallsStreaming(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	a, calls := travelAgent(t, buildGoogleLLM(t))

	var (
		text  strings.Builder
		final *agent.Result
	)

	for ev, err := range a.RunStream(ctx, llm.Text("What is the weather like where Ada lives?")) {
		if err != nil {
			skipIfOverloaded(t, err)
			t.Fatalf("RunStream: %v", err)
		}

		text.WriteString(ev.TextDelta)

		if ev.Type == agent.EventDone {
			final = ev.Result
		}
	}

	if final == nil || calls.Load() < 2 || !strings.Contains(strings.ToLower(final.TextContent()), "purple snow") {
		t.Errorf("calls = %d, streamed = %q, final = %+v", calls.Load(), text.String(), final)
	}
}

func TestGoogleAgent_ParallelToolCalls(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	type cityInput struct {
		City string `json:"city" jsonschema:"description=City name"`
	}

	planted := map[string]string{"oslo": "42 millimetres of rain", "lima": "a sandstorm"}

	tool, err := llm.NewTool("weather", "Returns today's weather in a city.", func(_ context.Context, in *cityInput) (string, error) {
		return planted[strings.ToLower(in.City)], nil
	})
	if err != nil {
		t.Fatal(err)
	}

	a, err := agent.New(buildRetryingGoogleLLM(t), "weather", "Answer with the weather tool only.")
	if err != nil {
		t.Fatal(err)
	}

	if err = a.AddTool(tool); err != nil {
		t.Fatal(err)
	}

	res, err := a.Run(ctx, llm.Text("What is the weather in Oslo and in Lima today?"))
	if err != nil {
		skipIfOverloaded(t, err)
		t.Fatalf("Run: %v", err)
	}

	answer := strings.ToLower(res.TextContent())
	if !strings.Contains(answer, "42") || !strings.Contains(answer, "sandstorm") {
		t.Errorf("answer = %q", res.TextContent())
	}
}

func TestGoogleLLM_ResponseFormat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	type capital struct {
		Country string `json:"country"`
		City    string `json:"city"`
	}

	format, err := llm.NewResponseFormat[capital]("capital", "A country and its capital.")
	if err != nil {
		t.Fatal(err)
	}

	res, err := buildRetryingGoogleLLM(t).Execute(ctx, []llm.Message{llm.UserMessage(llm.Text("The capital of Italy."))}, llm.WithResponseFormat(format))
	if err != nil {
		skipIfOverloaded(t, err)
		t.Fatal(err)
	}

	var got capital
	if err := json.Unmarshal([]byte(res.Message.TextContent()), &got); err != nil || !strings.EqualFold(got.City, "rome") {
		t.Errorf("answer = %q (%v)", res.Message.TextContent(), err)
	}
}
