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

package trace_test

import (
	"context"
	"testing"
	"time"

	"github.com/henomis/phero/trace"
)

// --- Event interface compliance ---

func TestEventTypes_ImplementEvent(t *testing.T) {
	now := time.Now()
	// Each concrete type must be assignable to trace.Event.
	events := []trace.Event{
		trace.AgentStartEvent{Timestamp: now},
		trace.AgentIterationEvent{Timestamp: now},
		trace.AgentRunSummaryEvent{Timestamp: now},
		trace.AgentEndEvent{Timestamp: now},
		trace.LLMRequestEvent{Timestamp: now},
		trace.LLMResponseEvent{Timestamp: now},
		trace.ToolCallEvent{Timestamp: now},
		trace.ToolResultEvent{Timestamp: now},
		trace.MemorySaveEvent{Timestamp: now},
		trace.MemoryRetrieveEvent{Timestamp: now},
	}
	if len(events) != 10 {
		t.Fatalf("expected 10 event types, got %d", len(events))
	}
}

// --- NoopTracer ---

func TestNoopTracer_DiscardEvents(t *testing.T) {
	// Should not panic for any event type.
	n := trace.Noop
	n.Trace(trace.AgentStartEvent{AgentName: "x", Timestamp: time.Now()})
	n.Trace(trace.ToolCallEvent{AgentName: "x", ToolName: "y", Timestamp: time.Now()})
}

// --- Context propagation ---

func TestWithTracer_FromContext(t *testing.T) {
	var got []trace.Event
	spy := &spyTracer{fn: func(e trace.Event) { got = append(got, e) }}

	ctx := trace.WithTracer(context.Background(), spy)
	tracer := trace.FromContext(ctx)
	tracer.Trace(trace.AgentStartEvent{AgentName: "a"})

	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if _, ok := got[0].(trace.AgentStartEvent); !ok {
		t.Errorf("expected AgentStartEvent, got %T", got[0])
	}
}

func TestFromContext_NoTracer_ReturnsNoop(t *testing.T) {
	tracer := trace.FromContext(context.Background())
	// Should not panic.
	tracer.Trace(trace.AgentEndEvent{AgentName: "a"})
}

// helpers

type spyTracer struct {
	fn func(trace.Event)
}

func (s *spyTracer) Trace(e trace.Event) { s.fn(e) }
