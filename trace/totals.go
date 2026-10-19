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
	"maps"
	"time"

	"github.com/henomis/phero/v2/llm"
)

// unknownModel keys usage whose model was not reported.
const unknownModel = "unknown"

// Totals aggregates a span and all of its descendants in the same process.
//
// Time fields are sums of span durations per category. Categories can overlap
// (a tool that runs a sub-agent counts in ToolTime and the sub-agent's model
// calls count in LLMTime), and parallel tools can sum to more than wall time.
type Totals struct {
	AgentRuns       int           `json:"agentRuns,omitempty"`
	LLMCalls        int           `json:"llmCalls"`
	ToolCalls       int           `json:"toolCalls"`
	ToolErrors      int           `json:"toolErrors"`
	MemoryRetrieved int           `json:"memoryRetrieved,omitempty"`
	MemorySaved     int           `json:"memorySaved,omitempty"`
	Usage           UsageSummary  `json:"usage"`
	LLMTime         time.Duration `json:"llmTime"`
	ToolTime        time.Duration `json:"toolTime"`
	MemoryTime      time.Duration `json:"memoryTime"`
	// ByModel breaks usage down by model.
	ByModel map[string]ModelTotals `json:"byModel,omitempty"`
	// ByAgent breaks the work down by the agent that did it.
	ByAgent map[string]AgentTotals `json:"byAgent,omitempty"`
	// ByTool breaks tool calls down by tool name.
	ByTool map[string]ToolTotals `json:"byTool,omitempty"`
}

// ModelTotals aggregates the calls made to one model.
type ModelTotals struct {
	Calls int          `json:"calls"`
	Usage UsageSummary `json:"usage"`
}

// AgentTotals aggregates the work done directly by one agent, excluding its
// sub-agents (which have their own entry).
type AgentTotals struct {
	Runs       int          `json:"runs"`
	LLMCalls   int          `json:"llmCalls"`
	ToolCalls  int          `json:"toolCalls"`
	ToolErrors int          `json:"toolErrors"`
	Usage      UsageSummary `json:"usage"`
}

// ToolTotals aggregates the calls to one tool.
type ToolTotals struct {
	Calls    int           `json:"calls"`
	Errors   int           `json:"errors"`
	Duration time.Duration `json:"duration"`
}

func (u *UsageSummary) add(o UsageSummary) {
	u.InputTokens += o.InputTokens
	u.OutputTokens += o.OutputTokens
	u.CacheReadTokens += o.CacheReadTokens
	u.CacheWriteTokens += o.CacheWriteTokens
	u.ReasoningTokens += o.ReasoningTokens
	u.CostUSD += o.CostUSD
}

func usageOf(u llm.Usage, cost float64) UsageSummary {
	return UsageSummary{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		ReasoningTokens:  u.ReasoningTokens,
		CostUSD:          cost,
	}
}

// clone returns a copy of t that shares no maps with it.
func (t *Totals) clone() Totals {
	c := *t
	c.ByModel = maps.Clone(t.ByModel)
	c.ByAgent = maps.Clone(t.ByAgent)
	c.ByTool = maps.Clone(t.ByTool)

	return c
}

// add merges o into t.
func (t *Totals) add(o *Totals) {
	t.AgentRuns += o.AgentRuns
	t.LLMCalls += o.LLMCalls
	t.ToolCalls += o.ToolCalls
	t.ToolErrors += o.ToolErrors
	t.MemoryRetrieved += o.MemoryRetrieved
	t.MemorySaved += o.MemorySaved
	t.Usage.add(o.Usage)
	t.LLMTime += o.LLMTime
	t.ToolTime += o.ToolTime
	t.MemoryTime += o.MemoryTime

	for k, v := range o.ByModel {
		t.model(k, func(m *ModelTotals) {
			m.Calls += v.Calls
			m.Usage.add(v.Usage)
		})
	}

	for k, v := range o.ByAgent {
		t.agent(k, func(a *AgentTotals) {
			a.Runs += v.Runs
			a.LLMCalls += v.LLMCalls
			a.ToolCalls += v.ToolCalls
			a.ToolErrors += v.ToolErrors
			a.Usage.add(v.Usage)
		})
	}

	for k, v := range o.ByTool {
		t.tool(k, func(tt *ToolTotals) {
			tt.Calls += v.Calls
			tt.Errors += v.Errors
			tt.Duration += v.Duration
		})
	}
}

func (t *Totals) model(name string, fn func(*ModelTotals)) {
	if t.ByModel == nil {
		t.ByModel = make(map[string]ModelTotals)
	}

	v := t.ByModel[name]
	fn(&v)
	t.ByModel[name] = v
}

func (t *Totals) agent(name string, fn func(*AgentTotals)) {
	if t.ByAgent == nil {
		t.ByAgent = make(map[string]AgentTotals)
	}

	v := t.ByAgent[name]
	fn(&v)
	t.ByAgent[name] = v
}

func (t *Totals) tool(name string, fn func(*ToolTotals)) {
	if t.ByTool == nil {
		t.ByTool = make(map[string]ToolTotals)
	}

	v := t.ByTool[name]
	fn(&v)
	t.ByTool[name] = v
}

// addOwn adds the contribution of a single span (not its children) to t.
func (t *Totals) addOwn(payload Payload, agent string, dur time.Duration, failed bool) {
	switch p := payload.(type) {
	case *Run:
		t.AgentRuns++
		t.agent(p.Agent, func(a *AgentTotals) { a.Runs++ })
	case *LLMCall:
		usage := usageOf(p.Usage, p.CostUSD)
		t.LLMCalls++
		t.Usage.add(usage)
		t.LLMTime += dur

		model := p.ResponseModel
		if model == "" {
			model = p.RequestModel
		}

		if model == "" {
			model = unknownModel
		}

		t.model(model, func(m *ModelTotals) {
			m.Calls++
			m.Usage.add(usage)
		})
		t.agent(agent, func(a *AgentTotals) {
			a.LLMCalls++
			a.Usage.add(usage)
		})
	case *ToolCall:
		t.ToolCalls++
		t.ToolTime += dur

		errs := 0
		if failed {
			errs = 1
		}

		t.ToolErrors += errs
		t.tool(p.Name, func(tt *ToolTotals) {
			tt.Calls++
			tt.Errors += errs
			tt.Duration += dur
		})
		t.agent(agent, func(a *AgentTotals) {
			a.ToolCalls++
			a.ToolErrors += errs
		})
	case *Memory:
		t.MemoryTime += dur
		if p.Op == MemorySave {
			t.MemorySaved += p.Count
		} else {
			t.MemoryRetrieved += p.Count
		}
	}
}
