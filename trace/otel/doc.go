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

// Package otel exports phero trace spans as OpenTelemetry spans that follow
// the OpenTelemetry GenAI semantic conventions.
//
// The package depends only on the OpenTelemetry API: the application
// configures the SDK (a TracerProvider and an exporter) and passes the
// provider in.
//
//	exp := otel.New(tp) // tp is an OpenTelemetry TracerProvider
//	p := trace.NewProvider(
//		trace.WithIDSource(exp.IDSource()),
//		trace.WithProcessor(exp),
//	)
//	a.SetTracing(p)
//
// With the IDSource the OpenTelemetry span is started together with the phero
// span, so both share trace and span IDs, and a run started under an
// OpenTelemetry span in ctx becomes its child. Without it the exporter still
// builds the same tree, with its own IDs.
//
// Span model: invoke_agent {agent} for runs, chat {model} for model calls,
// execute_tool {name} for tools, embeddings {model}, retrieval, and spans with
// phero.* attributes for handoffs, memory and middleware. Sub-agents nest
// under the tool call that ran them, and concurrent runs never share spans.
//
// Content (prompts, responses, tool arguments and results, retrieval
// documents) is not recorded unless WithContent(true) is set, as the
// conventions require. When enabled, gen_ai.input.messages holds the full
// prompt of each call and every content attribute is truncated to
// WithMaxAttrBytes bytes (64 KiB by default). Inline images are replaced by
// their type, size and sha256.
//
// Semantic conventions: OpenTelemetry semconv v1.41.0 (GenAI conventions in
// Development status). OTEL_SEMCONV_STABILITY_OPT_IN is not consulted.
package otel
