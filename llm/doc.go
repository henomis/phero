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

// Package llm provides small, composable building blocks for driving chat-based LLMs.
//
// It defines a minimal LLM interface, a generic FunctionTool helper for exposing
// Go functions as callable tools, and utilities for producing strict JSON Schema
// maps that match the OpenAI "strict" schema expectations.
//
// For convenience, this package also re-exports a handful of types and constants
// from github.com/sashabaranov/go-openai (e.g. Message, Tool, ToolCall and chat
// role constants) so higher-level packages can depend on a single import.
package llm
