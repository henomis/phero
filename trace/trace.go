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
	"time"

	"github.com/henomis/phero/llm"
)

// UsageSummary aggregates token consumption across a full agent run.
type UsageSummary struct {
	// InputTokens is the total number of prompt tokens sent to the model.
	InputTokens int
	// OutputTokens is the total number of completion tokens produced by the model.
	OutputTokens int
}

// LatencySummary aggregates time spent in each major phase of an agent run.
type LatencySummary struct {
	// Total is the full wall-clock duration of the run.
	Total time.Duration
	// LLM is the time spent waiting on model calls.
	LLM time.Duration
	// Tool is the time spent executing tool calls.
	Tool time.Duration
	// Memory is the time spent retrieving from and saving to memory.
	Memory time.Duration
}

// ToolCallSummary aggregates calls and failures for a single tool during a run.
type ToolCallSummary struct {
	// ToolName is the stable tool identifier exposed to the model.
	ToolName string
	// Calls is the total number of times the tool was invoked.
	Calls int
	// Errors is the number of failed invocations for the tool.
	Errors int
}

// RunSummary aggregates high-level observability data for a single agent run.
type RunSummary struct {
	// AgentName is the agent that produced this summary.
	AgentName string
	// Iterations is the number of agent loop iterations executed.
	Iterations int
	// LLMCalls is the number of model requests issued during the run.
	LLMCalls int
	// ToolCalls is the total number of tool invocations during the run.
	ToolCalls int
	// ToolErrors is the number of tool invocations that returned an error.
	ToolErrors int
	// MemoryRetrieved is the total number of messages loaded from memory.
	MemoryRetrieved int
	// MemorySaved is the total number of messages persisted to memory.
	MemorySaved int
	// Usage contains the aggregated token counts for the run.
	Usage UsageSummary
	// Latency contains the aggregated latency breakdown for the run.
	Latency LatencySummary
	// Tools contains per-tool call totals for the run.
	Tools []ToolCallSummary
	// HandoffAgent is set when the run ended by handing work to another agent.
	HandoffAgent string
	// Error is the final run error message, when the run did not complete cleanly.
	Error string
}

// Tracer receives trace events from an agent or LLM wrapper.
//
// Implementations must be safe to call from multiple goroutines concurrently.
// A nil Tracer is treated as a no-op by the agent.
type Tracer interface {
	Trace(event Event)
}

// Event is the common interface for all trace events.
//
// Use a type switch on the concrete type to inspect event-specific fields.
type Event interface {
	// traceEvent is an unexported marker method that prevents external types
	// from accidentally satisfying this interface.
	traceEvent()
}

// AgentStartEvent is emitted when an agent begins processing user input.
type AgentStartEvent struct {
	// AgentName is the name of the agent.
	AgentName string
	// Input is the user input string passed to Agent.Run.
	Input string
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (AgentStartEvent) traceEvent() {}

// AgentEndEvent is emitted when an agent finishes (successfully or with an error).
type AgentEndEvent struct {
	// AgentName is the name of the agent.
	AgentName string
	// Output is the agent's final response. Empty when Err is non-nil.
	Output string
	// Err is non-nil when the agent terminated due to an error.
	Err error
	// Iterations is the number of loop cycles that ran.
	Iterations int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (AgentEndEvent) traceEvent() {}

// AgentIterationEvent is emitted at the start of each agent loop iteration.
type AgentIterationEvent struct {
	// AgentName is the name of the agent.
	AgentName string
	// Iteration is the 1-based iteration counter.
	Iteration int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (AgentIterationEvent) traceEvent() {}

// AgentRunSummaryEvent is emitted once per run with aggregated observability data.
type AgentRunSummaryEvent struct {
	// Summary contains the aggregated metrics for the completed run.
	Summary RunSummary
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (AgentRunSummaryEvent) traceEvent() {}

// LLMRequestEvent is emitted just before the LLM is called.
type LLMRequestEvent struct {
	// AgentName is the originating agent's name. Empty when used standalone.
	AgentName string
	// MessageCount is the number of messages in the conversation.
	MessageCount int
	// ToolNames lists the names of tools passed in this call.
	ToolNames []string
	// Iteration is the agent loop iteration this request belongs to. Zero when used standalone.
	Iteration int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (LLMRequestEvent) traceEvent() {}

// LLMResponseEvent is emitted immediately after the LLM returns.
type LLMResponseEvent struct {
	// AgentName is the originating agent's name. Empty when used standalone.
	AgentName string
	// Message is the assistant message returned by the LLM.
	Message *llm.Message
	// Usage holds token counts for this call. Nil when the provider does not return usage.
	Usage *llm.Usage
	// Iteration is the agent loop iteration this response belongs to. Zero when used standalone.
	Iteration int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (LLMResponseEvent) traceEvent() {}

// ToolCallEvent is emitted just before a tool is invoked.
type ToolCallEvent struct {
	// AgentName is the name of the agent invoking the tool.
	AgentName string
	// ToolName is the name of the tool being called.
	ToolName string
	// Arguments is the raw JSON argument string from the model.
	Arguments string
	// CallID is the model-assigned tool call identifier.
	CallID string
	// Iteration is the agent loop iteration this call belongs to.
	Iteration int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (ToolCallEvent) traceEvent() {}

// ToolResultEvent is emitted after a tool returns.
type ToolResultEvent struct {
	// AgentName is the name of the agent that invoked the tool.
	AgentName string
	// ToolName is the name of the tool that was called.
	ToolName string
	// Result is the string result returned by the tool.
	Result string
	// Err is non-nil when the tool returned an error.
	Err error
	// CallID is the model-assigned tool call identifier.
	CallID string
	// Iteration is the agent loop iteration this result belongs to.
	Iteration int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (ToolResultEvent) traceEvent() {}

// MemorySaveEvent is emitted after messages are persisted to memory.
type MemorySaveEvent struct {
	// AgentName is the name of the agent saving to memory.
	AgentName string
	// Count is the number of messages saved.
	Count int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (MemorySaveEvent) traceEvent() {}

// MemoryRetrieveEvent is emitted after messages are retrieved from memory.
type MemoryRetrieveEvent struct {
	// AgentName is the name of the agent retrieving from memory.
	AgentName string
	// Count is the number of messages retrieved.
	Count int
	// Timestamp is when the event occurred.
	Timestamp time.Time
}

func (MemoryRetrieveEvent) traceEvent() {}
