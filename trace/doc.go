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

// Package trace records phero agent runs as trees of spans.
//
// Each operation is a span created with Start and finished with End: an agent
// run (*Run), a model call (*LLMCall), a tool call (*ToolCall), a handoff,
// memory access, retrieval, embeddings, LLM middleware work, or a *Custom span
// of your own. Spans are parented through the context, so concurrent runs
// never share spans and a sub-agent run as a tool nests under that tool call.
// The typed payload of a span holds the full input and output of the
// operation.
//
// A Provider delivers spans to Processors, called in order; several
// processors fan spans out to several backends:
//
//	p := trace.NewProvider(trace.WithProcessor(text.New(os.Stderr)))
//	a.SetTracing(p)                     // or ctx = trace.WithProvider(ctx, p)
//	defer p.Shutdown(ctx)
//
// Backends live in sub-packages: trace/text (colorized span tree),
// trace/jsonfile (NDJSON) and trace/otel (OpenTelemetry, GenAI semantic
// conventions). trace/tracetest records spans in memory for tests. Content,
// Redact and Batch wrap a processor to strip or mask content, or to export in
// the background.
//
// Each span aggregates the Totals of its descendants (calls, tokens, cost,
// time, per model, agent and tool). Agents build Result.Summary from the run
// span, so the summary includes sub-agents. Totals are recorded even without
// a provider; then nothing is exported.
//
// W3C trace context: Inject returns the traceparent of the current span and
// Extract makes a remote span the parent of the next one. LogAttrs returns the
// trace and span IDs for slog. WithSession and WithAttributes add a session
// ID and attributes to every span started from a context.
package trace
