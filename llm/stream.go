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

package llm

import (
	"context"
	"iter"
)

// StreamChunk is one incremental piece of a streaming LLM response.
//
// Consumers typically accumulate TextDelta (and ReasoningDelta) for live display,
// then read the complete assembled Message, Usage, and Model from the terminal
// chunk (the one with Done == true).
type StreamChunk struct {
	// TextDelta is an incremental piece of assistant text. Empty on non-text chunks.
	TextDelta string
	// ReasoningDelta is an incremental piece of extended-thinking text. Empty on non-reasoning chunks.
	ReasoningDelta string
	// ToolCall is a fully assembled tool call, emitted once the model finishes
	// streaming its arguments. Nil except on tool-call chunks.
	ToolCall *ToolCall
	// Message is the complete assembled assistant message. Set only on the terminal chunk.
	Message *Message
	// Usage holds token counts for the call. Set only on the terminal chunk, and
	// only when the provider reports usage.
	Usage *Usage
	// Model is the model that produced the response. Set only on the terminal chunk.
	Model string
	// Done reports whether this is the terminal chunk of the stream.
	Done bool
}

// StreamingLLM is implemented by chat-model backends that can stream their
// response incrementally, in addition to the buffered Execute.
type StreamingLLM interface {
	LLM
	// ExecuteStream returns an iterator over response chunks. The final chunk has
	// Done == true and carries the complete Message, Usage, and Model. If the
	// stream fails, the iterator yields a non-nil error and stops.
	ExecuteStream(ctx context.Context, messages []Message, tools []*Tool) iter.Seq2[StreamChunk, error]
}

// StreamOrBuffer streams from client when it implements StreamingLLM; otherwise
// it calls Execute once and yields a single terminal chunk built from the result.
//
// This lets callers consume any LLM uniformly: streaming backends produce
// incremental chunks, while buffered backends produce one terminal chunk whose
// TextDelta also carries the full text (so delta-only consumers still see content).
func StreamOrBuffer(ctx context.Context, client LLM, messages []Message, tools []*Tool) iter.Seq2[StreamChunk, error] {
	if s, ok := client.(StreamingLLM); ok {
		return s.ExecuteStream(ctx, messages, tools)
	}

	return func(yield func(StreamChunk, error) bool) {
		result, err := client.Execute(ctx, messages, tools)
		if err != nil {
			yield(StreamChunk{}, err)
			return
		}

		chunk := StreamChunk{Done: true}
		if result != nil {
			chunk.Message = result.Message
			chunk.Usage = result.Usage
			chunk.Model = result.Model
			if result.Message != nil {
				chunk.TextDelta = result.Message.TextContent()
			}
		}
		yield(chunk, nil)
	}
}
