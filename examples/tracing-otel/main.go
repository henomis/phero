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

// Package main exports the spans of a Phero agent run to OpenTelemetry,
// following the GenAI semantic conventions.
//
// To keep the example free of extra dependencies it records spans with the
// SDK's in-memory exporter and prints them. In an application, use an OTLP
// exporter (go.opentelemetry.io/otel/exporters/otlp/otlptrace) instead.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/openai"
	"github.com/henomis/phero/v2/trace"
	pherootel "github.com/henomis/phero/v2/trace/otel"
)

// WeatherInput is the input of the weather tool.
type WeatherInput struct {
	City string `json:"city" jsonschema:"description=The city to get the weather for"`
}

func weather(_ context.Context, in *WeatherInput) (string, error) {
	return "sunny, 22°C in " + in.City, nil
}

func main() {
	ctx := context.Background()

	// OpenTelemetry SDK wiring, owned by the application.
	mem := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(mem))

	// Phero exports its spans through trace/otel. Content capture is off by
	// default; it is turned on here to show the gen_ai.*.messages attributes.
	exp := pherootel.New(tp, pherootel.WithContent(true))
	tracing := trace.NewProvider(
		trace.WithIDSource(exp.IDSource()),
		trace.WithProcessor(exp),
		trace.WithResourceAttributes(trace.String("deployment.environment", "example")),
	)

	// Build LLM client from environment variables
	client, llmInfo := buildLLMFromEnv()
	fmt.Printf("LLM: %s\n\n", llmInfo)

	weatherTool, err := llm.NewTool("get_weather", "Returns the current weather of a city", weather)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm.NewTool: %v\n", err)
		os.Exit(1)
	}

	a, err := agent.New(client, "weather-agent", "You answer questions about the weather using the get_weather tool.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent.New: %v\n", err)
		os.Exit(1)
	}

	a.SetTracing(tracing)

	if err := a.AddTool(weatherTool); err != nil {
		fmt.Fprintf(os.Stderr, "AddTool: %v\n", err)
		os.Exit(1)
	}

	// The agent run nests under the application's own span, and carries the
	// session id and custom attributes taken from the context.
	ctx, request := tp.Tracer("example").Start(ctx, "handle request")
	ctx = trace.WithSession(ctx, "session-42")
	ctx = trace.WithAttributes(ctx, trace.String("user.id", "demo"))

	result, err := a.Run(ctx, llm.Text("What's the weather in Rome?"))

	request.End()

	if err != nil {
		fmt.Fprintf(os.Stderr, "Run: %v\n", err)
		os.Exit(1)
	}

	// Read the spans before shutdown: InMemoryExporter.Shutdown resets them.
	spans := mem.GetSpans()

	if err := tp.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
	}

	fmt.Printf("Result: %s\n", result.TextContent())
	fmt.Printf("Run: stop=%s llm_calls=%d tokens=%d/%d trace_id=%s\n\n",
		result.Summary.StopReason, result.Summary.LLMCalls,
		result.Summary.Usage.InputTokens, result.Summary.Usage.OutputTokens, result.Summary.TraceID)

	printSpans(spans)
}

// printSpans prints each exported span with its GenAI attributes.
func buildLLMFromEnv() (llm.LLM, string) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))

	// If no key and no base URL are set, assume a local OpenAI-compatible server (e.g. Ollama).
	if apiKey == "" && baseURL == "" {
		baseURL = openai.OllamaBaseURL
	}

	if model == "" {
		if baseURL == openai.OllamaBaseURL && apiKey == "" {
			model = "gpt-oss:20b-cloud"
		} else {
			model = openai.DefaultModel
		}
	}

	opts := []openai.Option{openai.WithModel(model)}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}

	client := openai.New(apiKey, opts...)

	info := fmt.Sprintf("model=%s base_url=%s", model, baseURL)
	if baseURL == "" {
		info = fmt.Sprintf("model=%s", model)
	}

	return client, info
}

func printSpans(spans tracetest.SpanStubs) {
	for _, s := range spans {
		fmt.Printf("%s  span=%s parent=%s  %s (%s)\n",
			s.SpanContext.TraceID(), s.SpanContext.SpanID(), s.Parent.SpanID(),
			s.Name, s.EndTime.Sub(s.StartTime).Round(time.Millisecond))

		for _, kv := range s.Attributes {
			if !strings.HasPrefix(string(kv.Key), "gen_ai.") {
				continue
			}

			value := kv.Value.String()
			if len(value) > 120 {
				value = value[:120] + "…"
			}

			fmt.Printf("    %s = %s\n", kv.Key, value)
		}
	}
}
