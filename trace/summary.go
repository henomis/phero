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
	"slices"
	"strings"
	"time"
)

// UsageSummary aggregates token consumption.
type UsageSummary struct {
	// InputTokens is the total number of prompt tokens sent to the model.
	InputTokens int `json:"inputTokens"`
	// OutputTokens is the total number of completion tokens produced by the model.
	OutputTokens int `json:"outputTokens"`
	// CacheReadTokens is the number of input tokens served from a prompt cache.
	CacheReadTokens int `json:"cacheReadTokens,omitempty"`
	// CacheWriteTokens is the number of input tokens written to a prompt cache.
	CacheWriteTokens int `json:"cacheWriteTokens,omitempty"`
	// ReasoningTokens is the part of OutputTokens spent on reasoning.
	ReasoningTokens int `json:"reasoningTokens,omitempty"`
	// CostUSD is the best-effort total cost in US dollars, computed from
	// per-model pricing. Zero when no pricing is known for the models used.
	CostUSD float64 `json:"costUsd"`
}

// LatencySummary aggregates time spent in each major phase of an agent run.
type LatencySummary struct {
	// Total is the full wall-clock duration of the run.
	Total time.Duration `json:"total"`
	// LLM is the time spent waiting on model calls.
	LLM time.Duration `json:"llm"`
	// Tool is the time spent executing tool calls.
	Tool time.Duration `json:"tool"`
	// Memory is the time spent retrieving from and saving to memory.
	Memory time.Duration `json:"memory"`
	// TimeToFirstToken is the time to first token of the first streamed model
	// call. Zero for buffered runs.
	TimeToFirstToken time.Duration `json:"timeToFirstToken,omitempty"`
}

// ToolCallSummary aggregates calls and failures for a single tool during a run.
type ToolCallSummary struct {
	// ToolName is the stable tool identifier exposed to the model.
	ToolName string `json:"toolName"`
	// Calls is the total number of times the tool was invoked.
	Calls int `json:"calls"`
	// Errors is the number of failed invocations for the tool.
	Errors int `json:"errors"`
	// Duration is the summed execution time of the tool's calls.
	Duration time.Duration `json:"duration"`
}

// RunSummary aggregates high-level observability data for a single agent run,
// including the work of any sub-agents run in the same process.
type RunSummary struct {
	// AgentName is the agent that produced this summary.
	AgentName string `json:"agentName"`
	// TraceID and SpanID identify the run span.
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
	// SessionID is the session identifier carried by the context, if any.
	SessionID string `json:"sessionId,omitempty"`
	// StopReason explains why the run ended.
	StopReason StopReason `json:"stopReason"`
	// Iterations is the number of agent loop iterations executed.
	Iterations int `json:"iterations"`
	// MaxIterations is the configured iteration limit; 0 means unlimited.
	MaxIterations int `json:"maxIterations"`
	// LLMCalls is the number of model requests issued during the run.
	LLMCalls int `json:"llmCalls"`
	// ToolCalls is the total number of tool invocations during the run.
	ToolCalls int `json:"toolCalls"`
	// ToolErrors is the number of tool invocations that returned an error.
	ToolErrors int `json:"toolErrors"`
	// MemoryRetrieved is the total number of messages loaded from memory.
	MemoryRetrieved int `json:"memoryRetrieved"`
	// MemorySaved is the total number of messages persisted to memory.
	MemorySaved int `json:"memorySaved"`
	// Usage contains the aggregated token counts for the run.
	Usage UsageSummary `json:"usage"`
	// Latency contains the aggregated latency breakdown for the run.
	Latency LatencySummary `json:"latency"`
	// Tools contains per-tool call totals for the run, sorted by name.
	Tools []ToolCallSummary `json:"tools,omitempty"`
	// ByModel breaks usage down by model.
	ByModel map[string]ModelTotals `json:"byModel,omitempty"`
	// ByAgent breaks the work down by agent, including sub-agents.
	ByAgent map[string]AgentTotals `json:"byAgent,omitempty"`
	// HandoffAgents lists the agents that the run handed work to; more than one means fan-out.
	HandoffAgents []string `json:"handoffAgents,omitempty"`
	// Error is the final run error message, when the run did not complete cleanly.
	Error string `json:"error,omitempty"`
}

// NewRunSummary builds the summary of an ended run span. It returns nil when
// s is nil or its payload is not a *Run.
func NewRunSummary(s *Span) *RunSummary {
	if s == nil {
		return nil
	}

	run, ok := s.payload.(*Run)
	if !ok {
		return nil
	}

	s.mu.Lock()
	t := s.totalsLocked()
	end := s.end
	err := s.err
	s.mu.Unlock()

	if end.IsZero() {
		end = time.Now()
	}

	summary := &RunSummary{
		AgentName:       run.Agent,
		TraceID:         s.sc.TraceID.String(),
		SpanID:          s.sc.SpanID.String(),
		SessionID:       run.SessionID,
		StopReason:      run.StopReason,
		Iterations:      run.Iterations,
		MaxIterations:   run.MaxIterations,
		LLMCalls:        t.LLMCalls,
		ToolCalls:       t.ToolCalls,
		ToolErrors:      t.ToolErrors,
		MemoryRetrieved: t.MemoryRetrieved,
		MemorySaved:     t.MemorySaved,
		Usage:           t.Usage,
		Latency: LatencySummary{
			Total:            end.Sub(s.start),
			LLM:              t.LLMTime,
			Tool:             t.ToolTime,
			Memory:           t.MemoryTime,
			TimeToFirstToken: run.TimeToFirstToken,
		},
		ByModel:       t.ByModel,
		ByAgent:       t.ByAgent,
		HandoffAgents: slices.Clone(run.Handoffs),
	}

	for name, tt := range t.ByTool {
		summary.Tools = append(summary.Tools, ToolCallSummary{
			ToolName: name, Calls: tt.Calls, Errors: tt.Errors, Duration: tt.Duration,
		})
	}

	slices.SortFunc(summary.Tools, func(a, b ToolCallSummary) int { return strings.Compare(a.ToolName, b.ToolName) })

	if err != nil {
		summary.Error = err.Error()
	}

	return summary
}
