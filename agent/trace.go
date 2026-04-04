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

package agent

import (
	"sort"
	"time"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/trace"
)

type runStats struct {
	agentName       string
	startedAt       time.Time
	llmCalls        int
	llmInputTokens  int
	llmOutputTokens int
	llmDuration     time.Duration
	toolCalls       int
	toolErrors      int
	toolDuration    time.Duration
	memoryRetrieved int
	memorySaved     int
	memoryDuration  time.Duration
	toolSummaries   map[string]*toolStats
}

type toolStats struct {
	calls  int
	errors int
}

func newRunStats(agentName string) *runStats {
	return &runStats{
		agentName:     agentName,
		startedAt:     time.Now(),
		toolSummaries: make(map[string]*toolStats),
	}
}

func (s *runStats) recordLLM(duration time.Duration, usage *llm.Usage) {
	s.llmCalls++
	s.llmDuration += duration
	if usage == nil {
		return
	}
	s.llmInputTokens += usage.InputTokens
	s.llmOutputTokens += usage.OutputTokens
}

func (s *runStats) recordTool(toolName string, err error, duration time.Duration) {
	s.toolCalls++
	s.toolDuration += duration

	stats, ok := s.toolSummaries[toolName]
	if !ok {
		stats = &toolStats{}
		s.toolSummaries[toolName] = stats
	}

	stats.calls++
	if err != nil {
		s.toolErrors++
		stats.errors++
	}
}

func (s *runStats) recordMemoryRetrieve(count int, duration time.Duration) {
	s.memoryDuration += duration
	s.memoryRetrieved += count
}

func (s *runStats) recordMemorySave(count int, duration time.Duration) {
	s.memoryDuration += duration
	s.memorySaved += count
}

func (s *runStats) summary(iterations int, handoffAgent string, err error) *trace.RunSummary {
	tools := make([]trace.ToolCallSummary, 0, len(s.toolSummaries))
	toolNames := make([]string, 0, len(s.toolSummaries))
	for toolName := range s.toolSummaries {
		toolNames = append(toolNames, toolName)
	}
	sort.Strings(toolNames)
	for _, toolName := range toolNames {
		stats := s.toolSummaries[toolName]
		tools = append(tools, trace.ToolCallSummary{
			ToolName: toolName,
			Calls:    stats.calls,
			Errors:   stats.errors,
		})
	}

	summary := &trace.RunSummary{
		AgentName:       s.agentName,
		Iterations:      iterations,
		LLMCalls:        s.llmCalls,
		ToolCalls:       s.toolCalls,
		ToolErrors:      s.toolErrors,
		MemoryRetrieved: s.memoryRetrieved,
		MemorySaved:     s.memorySaved,
		Usage: trace.UsageSummary{
			InputTokens:  s.llmInputTokens,
			OutputTokens: s.llmOutputTokens,
		},
		Latency: trace.LatencySummary{
			Total:  time.Since(s.startedAt),
			LLM:    s.llmDuration,
			Tool:   s.toolDuration,
			Memory: s.memoryDuration,
		},
		Tools:        tools,
		HandoffAgent: handoffAgent,
	}

	if err != nil {
		summary.Error = err.Error()
	}

	return summary
}
