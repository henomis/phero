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

package agent

import (
	"context"
	"iter"
	"time"

	"github.com/henomis/phero/v2/llm"
)

// EventType identifies the kind of an Event emitted by RunStream.
type EventType int

const (
	// EventTextDelta carries an incremental piece of the assistant's text answer.
	EventTextDelta EventType = iota
	// EventReasoningDelta carries an incremental piece of extended-thinking text.
	EventReasoningDelta
	// EventToolCall is emitted just before a tool call is executed.
	EventToolCall
	// EventToolResult is emitted after a tool call returns.
	EventToolResult
	// EventDone is the terminal event; its Result holds the final run result.
	EventDone
)

// Event is a single streaming event produced by Agent.RunStream.
type Event struct {
	// Type identifies which kind of event this is and therefore which fields are set.
	Type EventType
	// TextDelta holds incremental assistant text (EventTextDelta).
	TextDelta string
	// ReasoningDelta holds incremental reasoning text (EventReasoningDelta).
	ReasoningDelta string
	// ToolCallID correlates EventToolCall with its EventToolResult.
	ToolCallID string
	// ToolName is the tool being called or that returned (EventToolCall / EventToolResult).
	ToolName string
	// ToolArgs is the raw JSON argument string of a tool call (EventToolCall).
	ToolArgs string
	// ToolResult is the text result of a tool call (EventToolResult).
	ToolResult string
	// ToolError reports whether the tool call failed (EventToolResult).
	ToolError bool
	// Iteration is the 1-based agent loop iteration this event belongs to.
	Iteration int
	// Result holds the final agent result (EventDone), or the partial result
	// of a failed run, with its Summary, alongside the error.
	Result *Result
}

// emitFunc pushes an Event to a streaming consumer. It returns false once
// the consumer has stopped iterating, after which further calls are no-ops.
type emitFunc func(Event) bool

// RunStream runs the agent like Run, but streams progress as a sequence of
// Events instead of returning only the final result.
//
// It yields text and reasoning deltas as the model produces them, a ToolCall and
// ToolResult event around each tool invocation, and finally a single
// EventDone whose Result mirrors what Run would have returned. If the run
// fails, the iterator yields the error, with an Event whose Result is the
// partial result Run would have returned (Type is empty), and stops.
//
// Streaming uses the underlying LLM's incremental API when it implements
// llm.StreamingLLM; otherwise it transparently falls back to a single buffered
// response (see llm.StreamOrBuffer). Tracing records the same spans as Run.
func (a *Agent) RunStream(ctx context.Context, parts ...llm.ContentPart) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		stopped := false
		emit := func(ev Event) bool {
			if stopped {
				return false
			}

			if !yield(ev, nil) {
				stopped = true
				return false
			}

			return true
		}

		result, err := a.run(ctx, emit, parts...)

		if stopped {
			return
		}

		if err != nil {
			yield(Event{Result: result}, err)
			return
		}

		emit(Event{Type: EventDone, Result: result})
	}
}

// streamIteration performs a single streaming LLM call, emitting text and
// reasoning deltas, and returns the assembled result. It records the same
// LLM call span as the buffered path, plus the time to first token.
func (a *Agent) streamIteration(
	ctx context.Context, session []llm.Message, iteration int, rs *runState, emit emitFunc,
) (*llm.Result, error) {
	callCtx, span, call := a.startLLMCall(ctx, rs, session, iteration, true)
	start := time.Now()

	var (
		final    llm.StreamChunk
		gotFinal bool
	)

	for chunk, err := range llm.StreamOrBuffer(callCtx, a.llm, session, a.callOptions()...) {
		if err != nil {
			endLLMCall(span, call, nil, err)
			return nil, err
		}

		if call.FirstTokenAt.IsZero() && (chunk.TextDelta != "" || chunk.ReasoningDelta != "" || chunk.ToolCall != nil) {
			call.FirstTokenAt = time.Now()
			if rs.run.TimeToFirstToken == 0 {
				rs.run.TimeToFirstToken = call.FirstTokenAt.Sub(start)
			}
		}

		if chunk.TextDelta != "" {
			emit(Event{Type: EventTextDelta, TextDelta: chunk.TextDelta, Iteration: iteration})
		}

		if chunk.ReasoningDelta != "" {
			emit(Event{Type: EventReasoningDelta, ReasoningDelta: chunk.ReasoningDelta, Iteration: iteration})
		}

		if chunk.Done {
			final = chunk
			gotFinal = true
		}
	}

	if !gotFinal || final.Message == nil {
		endLLMCall(span, call, nil, ErrIncompleteStream)
		return nil, ErrIncompleteStream
	}

	result := &llm.Result{
		Message:         final.Message,
		Usage:           final.Usage,
		Model:           final.Model,
		FinishReason:    final.FinishReason,
		RawFinishReason: final.RawFinishReason,
	}
	endLLMCall(span, call, result, nil)

	return result, nil
}
