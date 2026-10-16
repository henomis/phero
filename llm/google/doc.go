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

// Package google provides an llm.LLM implementation backed by the Gemini API,
// through Google's official Go SDK (google.golang.org/genai).
//
// It converts phero messages (system/user/assistant/tool) into Gemini contents
// and Gemini function calls back into phero tool calls.
//
// # Thought signatures
//
// Gemini 3 models attach an opaque thought signature to the parts of an
// answer, and require the signature of each function call to come back,
// unchanged and on the same part, when the conversation continues; a request
// that omits it is rejected. phero's tool calls have no field for it, so the
// client keeps every signature in a reasoning part of the assistant message
// ([llm.Reasoning] with an empty text and the signature, tagged with the part
// it belongs to). Agents keep the assistant message whole between turns, so
// the signatures travel with it and the client puts each one back where
// Gemini placed it. Signature-only parts have no text, so they never show up
// in [llm.Message.ReasoningContent].
package google
