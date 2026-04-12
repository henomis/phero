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

// Package llm provides small, composable building blocks for driving LLM-backed
// applications.
//
// It defines a minimal chat-model interface, generic tool helpers for exposing
// Go functions to models, multimodal content types, and provider-neutral audio
// transcription and speech-synthesis interfaces.
//
// Provider packages such as llm/openai and llm/anthropic translate these types
// to and from their respective wire formats so higher-level packages can depend
// on a single, stable API.
package llm
