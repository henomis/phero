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
	"encoding/binary"
	"encoding/hex"
	"math/rand/v2"
	"time"
)

// TraceID is a W3C trace-context trace identifier.
type TraceID [16]byte //nolint:revive // W3C name, mirrors SpanID

// IsValid reports whether id is non-zero.
func (id TraceID) IsValid() bool { return id != TraceID{} }

// String returns the lowercase hex encoding of id.
func (id TraceID) String() string { return hex.EncodeToString(id[:]) }

// SpanID is a W3C trace-context span identifier.
type SpanID [8]byte

// IsValid reports whether id is non-zero.
func (id SpanID) IsValid() bool { return id != SpanID{} }

// String returns the lowercase hex encoding of id.
func (id SpanID) String() string { return hex.EncodeToString(id[:]) }

// SpanContext identifies a span within a trace, locally or across processes.
type SpanContext struct {
	// TraceID is the identifier shared by every span of the trace.
	TraceID TraceID
	// SpanID identifies the span within the trace.
	SpanID SpanID
	// Sampled mirrors the W3C sampled flag.
	Sampled bool
	// Remote is true when the context was extracted from a carrier.
	Remote bool
	// TraceState is the opaque W3C tracestate value, passed through unchanged.
	TraceState string
}

// IsValid reports whether both the trace and span identifiers are set.
func (sc SpanContext) IsValid() bool { return sc.TraceID.IsValid() && sc.SpanID.IsValid() }

// IDSource assigns the identity of new spans.
//
// The default source generates random W3C identifiers and inherits the trace
// ID from parent. trace/otel provides a source that starts the matching
// OpenTelemetry span so that phero and OTel share identifiers.
//
// Implementations must be safe for concurrent use.
type IDSource interface {
	// NewSpanContext returns the identity of a span of the given kind and name
	// starting at start. parent is the zero value for spans with no local or
	// remote parent. Returning an invalid SpanContext falls back to the
	// default source.
	NewSpanContext(ctx context.Context, parent SpanContext, kind Kind, name string, start time.Time) SpanContext
}

// newSpanContext returns a random span identity, inheriting the trace from parent.
func newSpanContext(parent SpanContext) SpanContext {
	sc := SpanContext{Sampled: true}

	if parent.IsValid() {
		sc.TraceID = parent.TraceID
		sc.Sampled = parent.Sampled
		sc.TraceState = parent.TraceState
	} else {
		for !sc.TraceID.IsValid() {
			binary.BigEndian.PutUint64(sc.TraceID[:8], rand.Uint64()) //nolint:gosec // IDs need uniqueness, not secrecy
			binary.BigEndian.PutUint64(sc.TraceID[8:], rand.Uint64()) //nolint:gosec // see above
		}
	}

	for !sc.SpanID.IsValid() {
		binary.BigEndian.PutUint64(sc.SpanID[:], rand.Uint64()) //nolint:gosec // see above
	}

	return sc
}
