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

// Package trace provides observability hooks for phero agents and LLM calls.
//
// A Tracer receives typed Event values at each significant lifecycle point:
// agent start/end, per-iteration boundaries, LLM request/response, tool
// call/result, and memory save/retrieve.
//
// Two implementations are provided:
//
//   - NoopTracer discards all events (zero cost, the default).
//   - TextTracer writes human-readable, colour-coded lines to an io.Writer.
//
// The tracer is propagated through the context using WithTracer and FromContext,
// so tool handlers can emit their own events without coupling to the agent struct.
//
// An LLM wrapper is available via NewLLM, which can be used independently of any
// agent to trace raw llm.LLM.Execute calls.
package trace
