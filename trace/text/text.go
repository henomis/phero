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
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/henomis/phero/trace"
)

// ANSI colour/style escape codes.
const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiDim       = "\033[2m"
	ansiRed       = "\033[31m"
	ansiGreen     = "\033[32m"
	ansiYellow    = "\033[33m"
	ansiBlue      = "\033[34m"
	ansiMagenta   = "\033[35m"
	ansiCyan      = "\033[36m"
	ansiWhite     = "\033[37m"
	ansiBoldWhite = "\033[1;37m"
	ansiBoldRed   = "\033[1;31m"
)

// Tracer writes human-readable, colour-coded trace lines to an io.Writer.
//
// Each line has the format:
//
//	HH:MM:SS.mmm [agent] ICON  detail
//
// Tracer is safe for concurrent use.
type Tracer struct {
	mu sync.Mutex
	w  io.Writer
}

// New returns a Tracer that writes to w.
//
// Pass os.Stderr (or os.Stdout) for terminal output.
func New(w io.Writer) *Tracer {
	return &Tracer{w: w}
}

// NewDefault returns a Tracer that writes to os.Stderr.
func NewDefault() *Tracer {
	return New(os.Stderr)
}

// Trace handles a single event and writes a formatted line to the writer.
func (t *Tracer) Trace(event trace.Event) {
	line := t.format(event)
	if line == "" {
		return
	}
	t.mu.Lock()
	_, _ = fmt.Fprintln(t.w, line)
	t.mu.Unlock()
}

func (t *Tracer) format(event trace.Event) string {
	switch e := event.(type) {
	case trace.AgentStartEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		return fmt.Sprintf("%s%s %s[%s]%s %s▶  AgentStart%s  input=%q",
			ansiBoldWhite, ts, ansiDim, e.AgentName, ansiReset,
			ansiBoldWhite, ansiReset,
			truncate(e.Input, 120))

	case trace.AgentEndEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		if e.Err != nil {
			return fmt.Sprintf("%s%s %s[%s]%s %s■  AgentEnd%s  iterations=%d error=%v",
				ansiBoldRed, ts, ansiDim, e.AgentName, ansiReset,
				ansiBoldRed, ansiReset,
				e.Iterations, e.Err)
		}
		return fmt.Sprintf("%s%s %s[%s]%s %s■  AgentEnd%s  iterations=%d output=%q",
			ansiBoldWhite, ts, ansiDim, e.AgentName, ansiReset,
			ansiBoldWhite, ansiReset,
			e.Iterations, truncate(e.Output, 120))

	case trace.AgentIterationEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		return fmt.Sprintf("%s%s %s[%s] ↻  iteration=%d%s",
			ansiDim, ts, ansiDim, e.AgentName,
			e.Iteration, ansiReset)

	case trace.AgentRunSummaryEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		summary := e.Summary
		tools := "none"
		if len(summary.Tools) > 0 {
			parts := make([]string, 0, len(summary.Tools))
			for _, tool := range summary.Tools {
				parts = append(parts, fmt.Sprintf("%s=%d/%d", tool.ToolName, tool.Calls, tool.Errors))
			}
			tools = strings.Join(parts, ", ")
		}
		handoff := ""
		if summary.HandoffAgent != "" {
			handoff = fmt.Sprintf(" handoff=%s", summary.HandoffAgent)
		}
		errText := ""
		if summary.Error != "" {
			errText = fmt.Sprintf(" error=%q", truncate(summary.Error, 120))
		}
		return fmt.Sprintf("%s%s %s[%s]%s %s≡  RunSummary%s  iterations=%d llm_calls=%d tool_calls=%d tool_errors=%d memory=%d/%d tokens=%d/%d latency(total=%s llm=%s tool=%s memory=%s) tools=[%s]%s%s",
			ansiBoldWhite, ts, ansiDim, summary.AgentName, ansiReset,
			ansiBoldWhite, ansiReset,
			summary.Iterations, summary.LLMCalls, summary.ToolCalls, summary.ToolErrors,
			summary.MemoryRetrieved, summary.MemorySaved,
			summary.Usage.InputTokens, summary.Usage.OutputTokens,
			summary.Latency.Total, summary.Latency.LLM, summary.Latency.Tool, summary.Latency.Memory,
			tools, handoff, errText)

	case trace.LLMRequestEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		agent := agentLabel(e.AgentName)
		iter := iterLabel(e.Iteration)
		tools := "none"
		if len(e.ToolNames) > 0 {
			tools = strings.Join(e.ToolNames, ", ")
		}
		return fmt.Sprintf("%s%s%s %s→  LLMRequest%s%s  messages=%d tools=[%s]",
			ansiBlue, ts, agent,
			ansiBold, ansiReset, iter,
			e.MessageCount, tools)

	case trace.LLMResponseEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		agent := agentLabel(e.AgentName)
		iter := iterLabel(e.Iteration)
		content := ""
		toolCalls := 0
		if e.Message != nil {
			content = truncate(e.Message.TextContent(), 120)
			toolCalls = len(e.Message.ToolCalls)
		}
		tokens := ""
		if e.Usage != nil {
			tokens = fmt.Sprintf(" in=%d out=%d", e.Usage.InputTokens, e.Usage.OutputTokens)
		}
		return fmt.Sprintf("%s%s%s %s←  LLMResponse%s%s%s  tool_calls=%d content=%q",
			ansiCyan, ts, agent,
			ansiBold, ansiReset, iter, tokens,
			toolCalls, content)

	case trace.ToolCallEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		return fmt.Sprintf("%s%s %s[%s]%s %s⚙  ToolCall%s  iter=%d tool=%s args=%s",
			ansiYellow, ts, ansiDim, e.AgentName, ansiReset,
			ansiYellow, ansiReset,
			e.Iteration, e.ToolName, truncate(e.Arguments, 200))

	case trace.ToolResultEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		if e.Err != nil {
			return fmt.Sprintf("%s%s %s[%s]%s %s✗  ToolResult%s  iter=%d tool=%s error=%v",
				ansiRed, ts, ansiDim, e.AgentName, ansiReset,
				ansiRed, ansiReset,
				e.Iteration, e.ToolName, e.Err)
		}
		return fmt.Sprintf("%s%s %s[%s]%s %s✓  ToolResult%s  iter=%d tool=%s result=%s",
			ansiGreen, ts, ansiDim, e.AgentName, ansiReset,
			ansiGreen, ansiReset,
			e.Iteration, e.ToolName, truncate(e.Result, 200))

	case trace.MemorySaveEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		return fmt.Sprintf("%s%s %s[%s]%s %s⟣  MemorySave%s  count=%d",
			ansiMagenta, ts, ansiDim, e.AgentName, ansiReset,
			ansiMagenta, ansiReset,
			e.Count)

	case trace.MemoryRetrieveEvent:
		ts := e.Timestamp.Format("15:04:05.000")
		return fmt.Sprintf("%s%s %s[%s]%s %s⟤  MemoryRetrieve%s  count=%d",
			ansiMagenta, ts, ansiDim, e.AgentName, ansiReset,
			ansiMagenta, ansiReset,
			e.Count)

	default:
		return ""
	}
}

// agentLabel returns " [name]" when name is non-empty, or "" for standalone usage.
func agentLabel(name string) string {
	if name == "" {
		return ""
	}
	return fmt.Sprintf(" %s[%s]%s", ansiDim, name, ansiReset)
}

// iterLabel returns " iter=N" when n > 0, or "" for standalone usage.
func iterLabel(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" %siter=%d%s", ansiDim, n, ansiReset)
}

// truncate shortens s to at most n runes, appending "…" if it was longer.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// Ensure Tracer implements trace.Tracer at compile time.
var _ trace.Tracer = (*Tracer)(nil)
