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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/henomis/phero/v2/llm"
)

// ContentPolicy decides which payload content reaches a processor.
type ContentPolicy struct {
	// Capture keeps prompts, responses, tool arguments and results, memory
	// messages and retrieval content. When false they are cleared.
	Capture bool
	// MaxBytes truncates each captured string to at most this many bytes.
	// Zero means unlimited.
	MaxBytes int
	// CaptureBinary keeps inline binary data (base64 images). When false each
	// binary part is replaced by a text part with its type, size and sha256.
	CaptureBinary bool
}

// truncationMark is appended to truncated strings.
const truncationMark = "…"

// Content returns a processor that applies policy to each span before passing
// it to next. Processors listed before or alongside it are not affected.
func Content(policy ContentPolicy, next Processor) Processor {
	return &contentProcessor{policy: policy, next: next}
}

type contentProcessor struct {
	policy ContentPolicy
	next   Processor
}

func (c *contentProcessor) OnStart(ctx context.Context, s *SpanData) {
	c.next.OnStart(ctx, c.apply(s))
}

func (c *contentProcessor) OnEnd(s *SpanData) {
	c.next.OnEnd(c.apply(s))
}

func (c *contentProcessor) Shutdown(ctx context.Context) error {
	return c.next.Shutdown(ctx)
}

func (c *contentProcessor) apply(s *SpanData) *SpanData {
	cp := *s
	if s.Payload != nil {
		cp.Payload = s.Payload.clone()
		cp.Payload.applyContent(c.policy)
	}

	return &cp
}

// Redact returns a processor that calls fn with a copy of each ended span
// before passing it to next. fn may change the copy freely, including its
// payload, without affecting other processors.
func Redact(fn func(*SpanData), next Processor) Processor {
	return &redactProcessor{fn: fn, next: next}
}

type redactProcessor struct {
	fn   func(*SpanData)
	next Processor
}

func (r *redactProcessor) OnStart(ctx context.Context, s *SpanData) {
	r.next.OnStart(ctx, s)
}

func (r *redactProcessor) OnEnd(s *SpanData) {
	cp := *s
	cp.Attrs = slices.Clone(s.Attrs)

	cp.Events = slices.Clone(s.Events)
	if s.Payload != nil {
		cp.Payload = s.Payload.clone()
	}

	r.fn(&cp)
	r.next.OnEnd(&cp)
}

func (r *redactProcessor) Shutdown(ctx context.Context) error {
	return r.next.Shutdown(ctx)
}

func (p ContentPolicy) str(s string) string {
	if !p.Capture {
		return ""
	}

	return p.truncate(s)
}

func (p ContentPolicy) truncate(s string) string {
	if p.MaxBytes <= 0 || len(s) <= p.MaxBytes {
		return s
	}

	cut := p.MaxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}

	return s[:cut] + truncationMark
}

func (p ContentPolicy) parts(parts []llm.ContentPart) []llm.ContentPart {
	if !p.Capture || len(parts) == 0 {
		return nil
	}

	out := make([]llm.ContentPart, len(parts))
	for i, part := range parts {
		out[i] = p.part(part)
	}

	return out
}

func (p ContentPolicy) part(part llm.ContentPart) llm.ContentPart {
	switch part.Type {
	case llm.ContentTypeImageBase64:
		if !p.CaptureBinary {
			return binarySummary(part)
		}

		return part
	case llm.ContentTypeImageURL:
		part.ImageURL = p.truncate(part.ImageURL)
		return part
	case llm.ContentTypeText, llm.ContentTypeReasoning, llm.ContentTypeRedactedReasoning:
		part.Text = p.truncate(part.Text)
		return part
	default:
		part.Text = p.truncate(part.Text)
		return part
	}
}

// binarySummary replaces inline binary data with a description of it.
func binarySummary(part llm.ContentPart) llm.ContentPart {
	size := base64.StdEncoding.DecodedLen(len(part.ImageBase64))
	sum := sha256.Sum256([]byte(part.ImageBase64))

	return llm.ContentPart{
		Type:     llm.ContentTypeText,
		MIMEType: part.MIMEType,
		Text: fmt.Sprintf("[%s %s ~%d bytes sha256:%s]",
			part.Type, part.MIMEType, size, hex.EncodeToString(sum[:])),
	}
}

func (p ContentPolicy) message(m llm.Message) llm.Message {
	m.Parts = p.parts(m.Parts)
	if len(m.ToolCalls) > 0 {
		calls := make([]llm.ToolCall, len(m.ToolCalls))
		for i, tc := range m.ToolCalls {
			tc.Function.Arguments = p.str(tc.Function.Arguments)
			calls[i] = tc
		}

		m.ToolCalls = calls
	}

	return m
}

func (p ContentPolicy) messages(msgs []llm.Message) []llm.Message {
	if !p.Capture || len(msgs) == 0 {
		return nil
	}

	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = p.message(m)
	}

	return out
}

func cloneMessage(m llm.Message) llm.Message {
	m.Parts = slices.Clone(m.Parts)
	m.ToolCalls = slices.Clone(m.ToolCalls)

	return m
}

func cloneMessages(msgs []llm.Message) []llm.Message {
	if msgs == nil {
		return nil
	}

	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = cloneMessage(m)
	}

	return out
}

func (r *Run) clone() Payload {
	cp := *r
	cp.Input = slices.Clone(r.Input)
	cp.Output = slices.Clone(r.Output)
	cp.Handoffs = slices.Clone(r.Handoffs)

	return &cp
}

func (r *Run) applyContent(p ContentPolicy) {
	r.Input = p.parts(r.Input)
	r.Output = p.parts(r.Output)
}

func (c *LLMCall) clone() Payload {
	cp := *c
	cp.Messages = cloneMessages(c.Messages)

	cp.Tools = slices.Clone(c.Tools)
	if c.Response != nil {
		resp := cloneMessage(*c.Response)
		cp.Response = &resp
	}

	if c.ToolChoice != nil {
		tc := *c.ToolChoice
		cp.ToolChoice = &tc
	}

	return &cp
}

func (c *LLMCall) applyContent(p ContentPolicy) {
	c.Messages = p.messages(c.Messages)
	if c.Response == nil {
		return
	}

	if !p.Capture {
		c.Response = nil
		return
	}

	resp := p.message(*c.Response)
	c.Response = &resp
}

func (t *ToolCall) clone() Payload {
	cp := *t
	return &cp
}

func (t *ToolCall) applyContent(p ContentPolicy) {
	t.Arguments = p.str(t.Arguments)
	t.Result = p.str(t.Result)
}

func (h *Handoff) clone() Payload {
	cp := *h
	return &cp
}

func (h *Handoff) applyContent(p ContentPolicy) {
	h.Context = p.str(h.Context)
}

func (m *Memory) clone() Payload {
	cp := *m
	cp.Messages = cloneMessages(m.Messages)

	return &cp
}

func (m *Memory) applyContent(p ContentPolicy) {
	m.Query = p.str(m.Query)
	m.Messages = p.messages(m.Messages)
}

func (r *Retrieval) clone() Payload {
	cp := *r
	if r.Hits != nil {
		cp.Hits = make([]Hit, len(r.Hits))
		for i, h := range r.Hits {
			h.Metadata = maps.Clone(h.Metadata)
			cp.Hits[i] = h
		}
	}

	return &cp
}

func (r *Retrieval) applyContent(p ContentPolicy) {
	r.Query = p.str(r.Query)
	for i := range r.Hits {
		r.Hits[i].Content = p.str(r.Hits[i].Content)
		if !p.Capture {
			r.Hits[i].Metadata = nil
		}
	}
}

func (e *Embedding) clone() Payload {
	cp := *e
	return &cp
}

func (*Embedding) applyContent(ContentPolicy) {}

func (m *Middleware) clone() Payload {
	cp := *m
	return &cp
}

func (*Middleware) applyContent(ContentPolicy) {}

func (c *Custom) clone() Payload {
	cp := *c
	cp.Data = maps.Clone(c.Data)

	return &cp
}

func (*Custom) applyContent(ContentPolicy) {}
