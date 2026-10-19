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

package tracetest

import (
	"context"
	"slices"
	"sync"

	"github.com/henomis/phero/v2/trace"
)

// Recorder is a trace.Processor that keeps every ended span in memory.
// It is safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	started []*trace.SpanData
	ended   []*trace.SpanData
}

var _ trace.Processor = (*Recorder)(nil)

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// OnStart records a started span.
func (r *Recorder) OnStart(_ context.Context, s *trace.SpanData) {
	r.mu.Lock()
	r.started = append(r.started, s)
	r.mu.Unlock()
}

// OnEnd records an ended span.
func (r *Recorder) OnEnd(s *trace.SpanData) {
	r.mu.Lock()
	r.ended = append(r.ended, s)
	r.mu.Unlock()
}

// Shutdown does nothing.
func (*Recorder) Shutdown(context.Context) error { return nil }

// Started returns the started spans in start order.
func (r *Recorder) Started() []*trace.SpanData {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.started)
}

// Spans returns the ended spans in end order.
func (r *Recorder) Spans() []*trace.SpanData {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.ended)
}

// ByKind returns the ended spans of kind k.
func (r *Recorder) ByKind(k trace.Kind) []*trace.SpanData {
	return r.filter(func(s *trace.SpanData) bool { return s.Kind == k })
}

// ByName returns the ended spans named name.
func (r *Recorder) ByName(name string) []*trace.SpanData {
	return r.filter(func(s *trace.SpanData) bool { return s.Name == name })
}

// Children returns the ended spans whose parent is sc.
func (r *Recorder) Children(sc trace.SpanContext) []*trace.SpanData {
	return r.filter(func(s *trace.SpanData) bool {
		return s.Parent.TraceID == sc.TraceID && s.Parent.SpanID == sc.SpanID
	})
}

// Reset discards all recorded spans.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.started, r.ended = nil, nil
	r.mu.Unlock()
}

func (r *Recorder) filter(fn func(*trace.SpanData) bool) []*trace.SpanData {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []*trace.SpanData

	for _, s := range r.ended {
		if fn(s) {
			out = append(out, s)
		}
	}

	return out
}
