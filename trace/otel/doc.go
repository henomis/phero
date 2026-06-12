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

// Package otel provides an OpenTelemetry-backed trace.Tracer that maps phero
// agent, LLM, and tool events onto OpenTelemetry spans.
//
// The package depends only on the OpenTelemetry API. The application is
// responsible for configuring the SDK (a TracerProvider and an exporter) and
// passing a tracer in, or for installing a global TracerProvider and using
// NewDefault. This keeps phero free of any exporter or SDK dependency.
//
//	// application wiring (SDK + exporter) is done by the caller, then:
//	tracer := otel.NewDefault() // uses the global TracerProvider
//	agent.SetTracer(tracer)
//
// Span model: an agent run becomes a root span; each LLM call and each tool
// call become child spans of that root. Token usage, cost, model name, and
// tool arguments are attached as span attributes following the OpenTelemetry
// GenAI semantic-convention attribute names where applicable.
//
// A single Tracer instance correlates events by agent name (and iteration or
// tool call ID). It therefore assumes that runs of a given agent name do not
// overlap concurrently on the same Tracer; sequential runs and handoffs to
// differently named agents are fully supported.
package otel
