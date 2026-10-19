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

package jsonfile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/henomis/phero/v2/trace"
)

// Record is the JSON object written for each span.
type Record struct {
	TraceID      string            `json:"traceId"`
	SpanID       string            `json:"spanId"`
	ParentSpanID string            `json:"parentSpanId,omitempty"`
	ParentRemote bool              `json:"parentRemote,omitempty"`
	Name         string            `json:"name"`
	Kind         trace.Kind        `json:"kind"`
	Start        time.Time         `json:"start"`
	End          time.Time         `json:"end"`
	DurationMS   float64           `json:"durationMs"`
	Error        string            `json:"error,omitempty"`
	Agent        string            `json:"agent,omitempty"`
	SessionID    string            `json:"sessionId,omitempty"`
	Attrs        []trace.Attr      `json:"attrs,omitempty"`
	Events       []trace.SpanEvent `json:"events,omitempty"`
	Links        []string          `json:"links,omitempty"`
	Totals       *trace.Totals     `json:"totals,omitempty"`
	Payload      trace.Payload     `json:"payload"`
}

// Processor appends one Record per ended span to a file. It is safe for
// concurrent use.
type Processor struct {
	mu     sync.Mutex
	f      *os.File
	enc    *json.Encoder
	err    error
	closed bool
}

var _ trace.Processor = (*Processor)(nil)

// New opens (or creates) the file at filePath in append mode. Shut the
// processor down (directly or through trace.Provider.Shutdown) to close it.
func New(filePath string) (*Processor, error) {
	if filePath == "" {
		return nil, ErrEmptyFilePath
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec,lll // path is supplied by the caller at construction time
	if err != nil {
		return nil, err
	}

	return &Processor{f: f, enc: json.NewEncoder(f)}, nil
}

// OnStart does nothing: spans are written when they end.
func (*Processor) OnStart(context.Context, *trace.SpanData) {}

// OnEnd writes s as one JSON line. The first write error is kept and returned
// by Shutdown.
func (p *Processor) OnEnd(s *trace.SpanData) {
	r := toRecord(s)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return
	}

	if err := p.enc.Encode(r); err != nil && p.err == nil {
		p.err = err
	}
}

// Shutdown closes the file and returns the first write error, if any.
func (p *Processor) Shutdown(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return p.err
	}

	p.closed = true

	return errors.Join(p.err, p.f.Close())
}

func toRecord(s *trace.SpanData) *Record {
	r := &Record{
		TraceID:    s.Context.TraceID.String(),
		SpanID:     s.Context.SpanID.String(),
		Name:       s.Name,
		Kind:       s.Kind,
		Start:      s.Start,
		End:        s.End,
		DurationMS: float64(s.Duration()) / float64(time.Millisecond),
		Agent:      s.Agent,
		SessionID:  s.SessionID,
		Attrs:      s.Attrs,
		Events:     s.Events,
		Payload:    s.Payload,
	}

	if s.Parent.SpanID.IsValid() {
		r.ParentSpanID = s.Parent.SpanID.String()
		r.ParentRemote = s.Parent.Remote
	}

	if s.Err != nil {
		r.Error = s.Err.Error()
	}

	for _, l := range s.Links {
		r.Links = append(r.Links, l.Traceparent())
	}

	if s.Totals.LLMCalls > 0 || s.Totals.ToolCalls > 0 || s.Totals.AgentRuns > 0 {
		t := s.Totals
		r.Totals = &t
	}

	return r
}
