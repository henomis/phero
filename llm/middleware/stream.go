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

package middleware

import (
	"iter"

	"github.com/henomis/phero/v2/llm"
)

// The middlewares implement llm.StreamingLLM, so wrapping a backend does not
// turn its streaming off. Each one streams from the LLM it wraps through
// llm.StreamOrBuffer, which also serves a wrapped LLM that cannot stream.
var (
	_ llm.StreamingLLM = (*retryLLM)(nil)
	_ llm.StreamingLLM = (*limiterLLM)(nil)
	_ llm.StreamingLLM = (*guardrailsLLM)(nil)
	_ llm.StreamingLLM = (*semanticCacheLLM)(nil)
)

// resultFromChunk returns the result carried by a stream's terminal chunk.
func resultFromChunk(c llm.StreamChunk) *llm.Result {
	return &llm.Result{
		Message:         c.Message,
		Usage:           c.Usage,
		Model:           c.Model,
		FinishReason:    c.FinishReason,
		RawFinishReason: c.RawFinishReason,
	}
}

// chunkFromResult returns the terminal chunk of a stream that delivers result
// at once, with its text as the delta, as llm.StreamOrBuffer does.
func chunkFromResult(result *llm.Result) llm.StreamChunk {
	chunk := llm.StreamChunk{Done: true}
	if result == nil {
		return chunk
	}

	chunk.Message = result.Message
	chunk.Usage = result.Usage
	chunk.Model = result.Model
	chunk.FinishReason = result.FinishReason
	chunk.RawFinishReason = result.RawFinishReason

	if result.Message != nil {
		chunk.TextDelta = result.Message.TextContent()
	}

	return chunk
}

// errorStream returns a stream that yields err and stops.
func errorStream(err error) iter.Seq2[llm.StreamChunk, error] {
	return func(yield func(llm.StreamChunk, error) bool) {
		yield(llm.StreamChunk{}, err)
	}
}
