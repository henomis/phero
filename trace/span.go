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
	"slices"
	"sync"
	"time"
)

// StartOption configures Start.
type StartOption func(*startConfig)

type startConfig struct {
	links []SpanContext
	start time.Time
}

// WithLink links the new span to sc, for example a fan-out join or a resumed run.
func WithLink(sc SpanContext) StartOption {
	return func(c *startConfig) { c.links = append(c.links, sc) }
}

// WithStartTime sets the start time of the new span instead of now.
func WithStartTime(t time.Time) StartOption {
	return func(c *startConfig) { c.start = t }
}

// Span is an operation within a trace. Create it with Start and finish it
// with End. A nil *Span is valid and all its methods are no-ops.
type Span struct {
	provider *Provider
	parent   *Span
	sc       SpanContext
	parentSC SpanContext
	name     string
	payload  Payload
	agent    string
	session  string
	start    time.Time
	links    []SpanContext
	ctxAttrs []Attr

	mu     sync.Mutex
	attrs  []Attr
	events []SpanEvent
	// totals aggregates the ended descendants. The span's own contribution
	// is kept apart (see ownTotals) so that leaf spans allocate no maps.
	totals Totals
	end    time.Time
	err    error
	ended  bool
}

// Start begins a span named name, parented to the current span in ctx or to a
// remote parent set with Extract. Its kind comes from the payload type; a nil
// payload is recorded as an empty *Custom. The returned context carries the
// new span. Start never returns a nil span.
//
// The provider is taken from ctx (see WithProvider), falling back to the
// parent span's provider. Without a provider the span is still recorded for
// totals and summaries, but nothing is exported.
func Start(ctx context.Context, name string, payload Payload, opts ...StartOption) (context.Context, *Span) {
	var cfg startConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.start.IsZero() {
		cfg.start = time.Now()
	}

	if payload == nil {
		payload = &Custom{}
	}

	parent := SpanFrom(ctx)

	parentSC, _ := ctx.Value(remoteKey).(SpanContext)
	if parent != nil {
		parentSC = parent.sc
	}

	p := ProviderFrom(ctx)
	if p == nil && parent != nil {
		p = parent.provider
	}

	var sc SpanContext
	if p.recording() && p.ids != nil {
		sc = p.ids.NewSpanContext(ctx, parentSC, payload.kind(), name, cfg.start)
	}

	if !sc.IsValid() {
		sc = newSpanContext(parentSC)
	}

	s := &Span{
		provider: p,
		parent:   parent,
		sc:       sc,
		parentSC: parentSC,
		name:     name,
		payload:  payload,
		session:  SessionFrom(ctx),
		start:    cfg.start,
		links:    append(cfg.links, linksFrom(ctx)...),
		ctxAttrs: attributesFrom(ctx),
	}

	if run, ok := payload.(*Run); ok {
		s.agent = run.Agent
	} else if parent != nil {
		s.agent = parent.agent
	}

	if p.recording() {
		p.onStart(ctx, s.data(false))
	}

	ctx = context.WithValue(ctx, spanKey, s)
	if len(linksFrom(ctx)) > 0 {
		ctx = context.WithValue(ctx, linksKey, []SpanContext(nil))
	}

	return ctx, s
}

// SpanFrom returns the current span in ctx, or nil when there is none. The
// methods of a nil *Span are no-ops.
func SpanFrom(ctx context.Context) *Span {
	s, _ := ctx.Value(spanKey).(*Span)
	return s
}

// PayloadFrom returns the payload of the nearest span in ctx, starting from
// the current one, whose payload has type T. Middleware uses it to annotate
// the enclosing *LLMCall.
func PayloadFrom[T Payload](ctx context.Context) (T, bool) {
	for s := SpanFrom(ctx); s != nil; s = s.parent {
		if p, ok := s.payload.(T); ok {
			return p, true
		}
	}

	var zero T

	return zero, false
}

// Context returns the identity of the span.
func (s *Span) Context() SpanContext {
	if s == nil {
		return SpanContext{}
	}

	return s.sc
}

// Name returns the span name.
func (s *Span) Name() string {
	if s == nil {
		return ""
	}

	return s.name
}

// Payload returns the span payload.
func (s *Span) Payload() Payload {
	if s == nil {
		return nil
	}

	return s.payload
}

// SetAttributes adds attributes to the span.
func (s *Span) SetAttributes(attrs ...Attr) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.attrs = append(s.attrs, attrs...)
	s.mu.Unlock()
}

// AddEvent records a point-in-time event on the span.
func (s *Span) AddEvent(name string, attrs ...Attr) {
	if s == nil {
		return
	}

	s.mu.Lock()
	s.events = append(s.events, SpanEvent{Name: name, Time: time.Now(), Attrs: attrs})
	s.mu.Unlock()
}

// Totals returns the aggregated totals of the span and its ended descendants.
// After End the value is final.
func (s *Span) Totals() Totals {
	if s == nil {
		return Totals{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.totalsLocked()
}

// totalsLocked returns a copy of the descendants' totals plus, once the span
// has ended, its own contribution. s.mu must be held.
func (s *Span) totalsLocked() Totals {
	t := s.totals.clone()
	if s.ended {
		t.addOwn(s.payload, s.agent, s.end.Sub(s.start), s.err != nil)
	}

	return t
}

// End finishes the span. err == nil means the operation succeeded. End is
// idempotent: only the first call has an effect.
//
// The span's totals, including its own contribution, are added to its parent.
// Descendants that end after their ancestor has ended are not rolled up.
func (s *Span) End(err error) {
	if s == nil {
		return
	}

	now := time.Now()

	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}

	s.ended = true
	s.end = now
	s.err = err
	s.mu.Unlock()

	if s.parent != nil {
		s.parent.addChild(s)
	}

	if s.provider.recording() {
		s.provider.onEnd(s.data(true))
	}
}

// addChild rolls the totals of the ended span c, and its own contribution,
// into s. c is no longer changed once ended, so it is read without its lock.
func (s *Span) addChild(c *Span) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return
	}

	s.totals.add(&c.totals)
	s.totals.addOwn(c.payload, c.agent, c.end.Sub(c.start), c.err != nil)
}

// data builds the SpanData view. When ended is true it includes the end state.
func (s *Span) data(ended bool) *SpanData {
	d := &SpanData{
		Context:   s.sc,
		Parent:    s.parentSC,
		Name:      s.name,
		Kind:      s.payload.kind(),
		Start:     s.start,
		Agent:     s.agent,
		SessionID: s.session,
		Links:     s.links,
		Payload:   s.payload,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	d.Attrs = make([]Attr, 0, len(s.provider.attrs)+len(s.ctxAttrs)+len(s.attrs))
	d.Attrs = append(d.Attrs, s.provider.attrs...)
	d.Attrs = append(d.Attrs, s.ctxAttrs...)
	d.Attrs = append(d.Attrs, s.attrs...)

	if ended {
		d.End = s.end
		d.Err = s.err
		d.Events = slices.Clone(s.events)
		d.Totals = s.totalsLocked()
	}

	return d
}
