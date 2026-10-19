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

package trace

import (
	"context"
	"time"
)

// SpanEvent is a point-in-time event recorded on a span.
type SpanEvent struct {
	Name  string    `json:"name"`
	Time  time.Time `json:"time"`
	Attrs []Attr    `json:"attrs,omitempty"`
}

// SpanData is the read-only view of a span delivered to processors.
//
// Processors must not change it or its payload; use Redact or Content to work
// on a copy.
type SpanData struct {
	// Context identifies the span.
	Context SpanContext
	// Parent identifies the parent span; zero for roots. Parent.Remote is set
	// when the parent was extracted from a carrier.
	Parent SpanContext
	Name   string
	Kind   Kind
	Start  time.Time
	// End is zero in OnStart.
	End time.Time
	// Err is the error the span ended with, or nil.
	Err error
	// Agent is the name of the agent run the span belongs to, if any.
	Agent string
	// SessionID is the session identifier carried by the context, if any.
	SessionID string
	// Attrs holds the provider, context and span attributes, in that order.
	Attrs   []Attr
	Events  []SpanEvent
	Links   []SpanContext
	Payload Payload
	// Totals aggregates the span and its descendants. Zero in OnStart.
	Totals Totals
}

// Duration returns End - Start, or zero for a span that has not ended.
func (d *SpanData) Duration() time.Duration {
	if d.End.IsZero() {
		return 0
	}

	return d.End.Sub(d.Start)
}

// Processor receives spans as they start and end.
//
// OnStart and OnEnd are called synchronously on the goroutine that starts or
// ends the span, possibly concurrently. Implementations must be safe for
// concurrent use and must not block; wrap exporters that do I/O in Batch.
type Processor interface {
	// OnStart is called when a span starts. It is meant for live views.
	OnStart(ctx context.Context, s *SpanData)
	// OnEnd is called when a span ends.
	OnEnd(s *SpanData)
	// Shutdown flushes pending work and releases resources.
	Shutdown(ctx context.Context) error
}
