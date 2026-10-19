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

package text

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

// ANSI colour/style escape codes.
const (
	ansiReset   = "\033[0m"
	ansiBold    = "\033[1m"
	ansiDim     = "\033[2m"
	ansiRed     = "\033[31m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiBlue    = "\033[34m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
)

const (
	indentUnit = "  "
	timeLayout = "15:04:05.000"
)

// Processor prints spans as an indented tree. It is safe for concurrent use.
type Processor struct {
	mu      sync.Mutex
	w       io.Writer
	verbose bool
	maxLen  int
	depth   map[trace.SpanID]int
}

var _ trace.Processor = (*Processor)(nil)

// Option configures a Processor.
type Option func(*Processor)

// WithVerbose prints a line when every span starts, not only agent runs.
func WithVerbose() Option {
	return func(p *Processor) { p.verbose = true }
}

// WithMaxLen truncates printed content to n runes. Zero (the default) prints
// everything.
func WithMaxLen(n int) Option {
	return func(p *Processor) { p.maxLen = n }
}

// New returns a Processor that writes to w.
func New(w io.Writer, opts ...Option) *Processor {
	p := &Processor{w: w, depth: make(map[trace.SpanID]int)}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// NewDefault returns a Processor that writes to os.Stderr.
func NewDefault(opts ...Option) *Processor {
	return New(os.Stderr, opts...)
}

// OnStart records the span depth and prints a start line for agent runs, or
// for every span in verbose mode.
func (p *Processor) OnStart(_ context.Context, s *trace.SpanData) {
	p.mu.Lock()
	defer p.mu.Unlock()

	depth := 0
	if d, ok := p.depth[s.Parent.SpanID]; ok {
		depth = d + 1
	}

	p.depth[s.Context.SpanID] = depth

	if s.Kind != trace.KindRun && !p.verbose {
		return
	}

	p.writeLine(s.Start, depth, ansiBlue+"▶"+ansiReset, ansiBold+s.Name+ansiReset, p.startDetail(s))
}

// OnEnd prints the span's end line and its events.
func (p *Processor) OnEnd(s *trace.SpanData) {
	p.mu.Lock()
	defer p.mu.Unlock()

	depth := p.depth[s.Context.SpanID]
	delete(p.depth, s.Context.SpanID)

	icon := ansiGreen + "✔" + ansiReset
	if s.Err != nil {
		icon = ansiRed + "✖" + ansiReset
	}

	detail := p.endDetail(s)
	if s.Err != nil {
		detail = strings.TrimSpace(detail + " " + ansiRed + "err=" + p.cut(s.Err.Error()) + ansiReset)
	}

	head := fmt.Sprintf("%s%s%s %s(%s)%s",
		ansiBold, s.Name, ansiReset, ansiDim, s.Duration().Round(time.Microsecond), ansiReset)
	p.writeLine(s.End, depth, icon, head, detail)

	for _, e := range s.Events {
		p.writeLine(e.Time, depth+1, ansiYellow+"•"+ansiReset, e.Name, formatAttrs(e.Attrs))
	}
}

// Shutdown does nothing; output is written synchronously.
func (*Processor) Shutdown(context.Context) error { return nil }

func (p *Processor) writeLine(t time.Time, depth int, icon, head, detail string) {
	line := fmt.Sprintf("%s%s%s %s%s %s",
		ansiDim, t.Format(timeLayout), ansiReset, strings.Repeat(indentUnit, depth), icon, head)
	if detail != "" {
		line += " " + detail
	}

	_, _ = fmt.Fprintln(p.w, line)
}

func (p *Processor) startDetail(s *trace.SpanData) string {
	switch pl := s.Payload.(type) {
	case *trace.Run:
		return kv("input", p.cut(llm.TextContent(pl.Input...)))
	case *trace.ToolCall:
		return kv("args", p.cut(pl.Arguments))
	default:
		return ""
	}
}

func (p *Processor) endDetail(s *trace.SpanData) string {
	switch pl := s.Payload.(type) {
	case *trace.Run:
		return p.runDetail(pl, &s.Totals)
	case *trace.LLMCall:
		return p.llmDetail(pl)
	case *trace.ToolCall:
		return join(kv("args", p.cut(pl.Arguments)), kv("result", p.cut(pl.Result)))
	case *trace.Handoff:
		return join(pl.From+" → "+pl.To, kv("context", p.cut(pl.Context)))
	case *trace.Memory:
		return join(kv("op", string(pl.Op)), fmt.Sprintf("count=%d", pl.Count), kv("query", p.cut(pl.Query)))
	case *trace.Retrieval:
		return join(kv("query", p.cut(pl.Query)), fmt.Sprintf("top_k=%d hits=%d", pl.TopK, len(pl.Hits)))
	case *trace.Embedding:
		return join(kv("model", pl.Model), fmt.Sprintf("inputs=%d", pl.Inputs))
	case *trace.Middleware:
		return p.middlewareDetail(pl)
	case *trace.Custom:
		return kv("type", pl.Type)
	default:
		return ""
	}
}

func (p *Processor) runDetail(r *trace.Run, t *trace.Totals) string {
	parts := []string{
		ansiCyan + "stop=" + string(r.StopReason) + ansiReset,
		fmt.Sprintf("iterations=%d llm_calls=%d tool_calls=%d tool_errors=%d",
			r.Iterations, t.LLMCalls, t.ToolCalls, t.ToolErrors),
		fmt.Sprintf("tokens=%d/%d", t.Usage.InputTokens, t.Usage.OutputTokens),
	}

	if t.Usage.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("cost=$%.6f", t.Usage.CostUSD))
	}

	if r.TimeToFirstToken > 0 {
		parts = append(parts, "ttft="+r.TimeToFirstToken.Round(time.Millisecond).String())
	}

	if len(r.Handoffs) > 0 {
		parts = append(parts, "handoff="+strings.Join(r.Handoffs, ","))
	}

	parts = append(parts, kv("output", p.cut(llm.TextContent(r.Output...))))

	return join(parts...)
}

func (p *Processor) llmDetail(c *trace.LLMCall) string {
	model := c.ResponseModel
	if model == "" {
		model = c.RequestModel
	}

	parts := []string{
		kv("model", model),
		fmt.Sprintf("tokens=%d/%d", c.Usage.InputTokens, c.Usage.OutputTokens),
		kv("finish", string(c.FinishReason)),
	}

	if c.CacheHit {
		parts = append(parts, ansiMagenta+"cache_hit"+ansiReset)
	}

	if c.Response != nil {
		names := make([]string, 0, len(c.Response.ToolCalls))
		for _, tc := range c.Response.ToolCalls {
			names = append(names, tc.Function.Name)
		}

		if len(names) > 0 {
			parts = append(parts, "tool_calls="+strings.Join(names, ","))
		}

		parts = append(parts,
			kv("reasoning", p.cut(c.Response.ReasoningContent())),
			kv("text", p.cut(c.Response.TextContent())),
		)
	}

	return join(parts...)
}

func (p *Processor) middlewareDetail(m *trace.Middleware) string {
	parts := []string{kv("outcome", m.Outcome)}
	if m.Attempt > 0 {
		parts = append(parts, fmt.Sprintf("attempt=%d", m.Attempt))
	}

	if m.Wait > 0 {
		parts = append(parts, "wait="+m.Wait.Round(time.Millisecond).String())
	}

	return join(append(parts, kv("rule", m.Rule), kv("reason", p.cut(m.Reason)))...)
}

func (p *Processor) cut(s string) string {
	if p.maxLen <= 0 {
		return s
	}

	runes := []rune(s)
	if len(runes) <= p.maxLen {
		return s
	}

	return string(runes[:p.maxLen]) + "…"
}

// kv formats key=value, or "" when value is empty.
func kv(key, value string) string {
	if value == "" {
		return ""
	}

	if strings.ContainsAny(value, " \n\t") {
		value = fmt.Sprintf("%q", value)
	}

	return key + "=" + value
}

func join(parts ...string) string {
	out := parts[:0]

	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}

	return strings.Join(out, " ")
}

func formatAttrs(attrs []trace.Attr) string {
	parts := make([]string, 0, len(attrs))
	for _, a := range attrs {
		parts = append(parts, kv(a.Key, fmt.Sprint(a.Value)))
	}

	return join(parts...)
}
