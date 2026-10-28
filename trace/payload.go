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

	"github.com/henomis/phero/v2/llm"
)

// Kind classifies a span by its payload type.
type Kind string

const (
	// KindRun is an agent run.
	KindRun Kind = "invoke_agent"
	// KindLLM is a single model call.
	KindLLM Kind = "chat"
	// KindTool is a single tool execution.
	KindTool Kind = "execute_tool"
	// KindHandoff is a handoff from one agent to another.
	KindHandoff Kind = "handoff"
	// KindMemory is a memory retrieve or save.
	KindMemory Kind = "memory"
	// KindRetrieval is a semantic search over a vector store.
	KindRetrieval Kind = "retrieval"
	// KindEmbedding is an embedding request.
	KindEmbedding Kind = "embeddings"
	// KindMiddleware is LLM middleware work such as a retry or a limiter wait.
	KindMiddleware Kind = "middleware"
	// KindCustom is a user- or framework-defined span.
	KindCustom Kind = "custom"
)

// Payload is the typed data of a span. The set of payloads is closed; use
// Custom for anything else.
//
// The caller fills the payload until the span ends. After End the payload
// belongs to the processors and must not be changed.
type Payload interface {
	kind() Kind
	clone() Payload
	applyContent(p ContentPolicy)
}

// StopReason explains why an agent run ended.
type StopReason string

const (
	// StopFinal means the model produced a final answer.
	StopFinal StopReason = "final"
	// StopMaxIterations means the run hit its iteration limit.
	StopMaxIterations StopReason = "max_iterations"
	// StopHandoff means the run ended by handing off to other agents.
	StopHandoff StopReason = "handoff"
	// StopRequested means a tool stopped the run on purpose (agent.StopRun).
	StopRequested StopReason = "stopped"
	// StopCancelled means the context was cancelled.
	StopCancelled StopReason = "cancelled"
	// StopGuardrail means a guardrail blocked a model call.
	StopGuardrail StopReason = "guardrail"
	// StopError means the run failed with any other error.
	StopError StopReason = "error"
)

// Run is the payload of an agent run span.
type Run struct {
	// Agent is the agent name.
	Agent string `json:"agent"`
	// Description is the agent description.
	Description string `json:"description,omitempty"`
	// SessionID is the session identifier carried by the context, if any.
	SessionID string `json:"sessionId,omitempty"`
	// Input is the user input of the run.
	Input []llm.ContentPart `json:"input,omitempty"`
	// Output is the final output of the run.
	Output []llm.ContentPart `json:"output,omitempty"`
	// Iterations is the number of loop iterations executed.
	Iterations int `json:"iterations"`
	// MaxIterations is the iteration limit; 0 means unlimited.
	MaxIterations int `json:"maxIterations"`
	// StopReason explains why the run ended.
	StopReason StopReason `json:"stopReason"`
	// Handoffs lists the agents the run handed off to.
	Handoffs []string `json:"handoffs,omitempty"`
	// TimeToFirstToken is the time to first token of the first streamed model
	// call, measured from that call's start. Zero for buffered runs.
	TimeToFirstToken time.Duration `json:"timeToFirstToken,omitempty"`
}

// ToolDef describes a tool offered to the model.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// LLMCall is the payload of a model call span.
type LLMCall struct {
	// Provider is the provider name (openai, anthropic, google), when known.
	Provider string `json:"provider,omitempty"`
	// RequestModel is the configured model, when known.
	RequestModel string `json:"requestModel,omitempty"`
	// ResponseModel is the model reported by the provider.
	ResponseModel string `json:"responseModel,omitempty"`
	// Iteration is the agent loop iteration, starting at 1.
	Iteration int `json:"iteration,omitempty"`
	// MessageOffset is the index in the full prompt of Messages[0]. The full
	// prompt is the concatenation of the deltas of the run's earlier calls.
	MessageOffset int `json:"messageOffset"`
	// Messages holds the prompt messages added since the previous call of the
	// same run (the whole prompt for the first call).
	Messages []llm.Message `json:"messages,omitempty"`
	// Tools lists the tools offered to the model.
	Tools []ToolDef `json:"tools,omitempty"`
	// ToolChoice is the tool choice option, if set.
	ToolChoice *llm.ToolChoice `json:"toolChoice,omitempty"`
	// ResponseFormat is the name of the structured output format, if set.
	ResponseFormat string `json:"responseFormat,omitempty"`
	// Response is the message returned by the model.
	Response *llm.Message `json:"response,omitempty"`
	// Usage is the token usage reported by the provider.
	Usage llm.Usage `json:"usage"`
	// CostUSD is the best-effort cost of the call.
	CostUSD float64 `json:"costUsd,omitempty"`
	// FinishReason is the normalised finish reason.
	FinishReason llm.FinishReason `json:"finishReason,omitempty"`
	// RawFinishReason is the provider's original finish reason.
	RawFinishReason string `json:"rawFinishReason,omitempty"`
	// Streaming is true when the call used the streaming API.
	Streaming bool `json:"streaming,omitempty"`
	// FirstTokenAt is when the first streamed chunk arrived.
	FirstTokenAt time.Time `json:"firstTokenAt,omitzero"`
	// CacheHit is true when a response cache answered the call.
	CacheHit bool `json:"cacheHit,omitempty"`
}

// SetResult records the model result on c. The cost is priced on the response
// model, falling back to the request model when the provider does not echo
// it; a cache hit costs nothing.
func (c *LLMCall) SetResult(res *llm.Result) {
	if res == nil {
		return
	}

	c.Response = res.Message
	c.ResponseModel = res.Model
	c.FinishReason = res.FinishReason
	c.RawFinishReason = res.RawFinishReason

	if res.Usage != nil {
		c.Usage = *res.Usage
	}

	if c.CacheHit {
		return
	}

	model := c.ResponseModel
	if model == "" {
		model = c.RequestModel
	}

	c.CostUSD = c.Usage.Cost(model)
}

// ToolCall is the payload of a tool execution span.
type ToolCall struct {
	// Name is the tool name.
	Name string `json:"name"`
	// CallID is the provider tool-call identifier.
	CallID string `json:"callId,omitempty"`
	// Iteration is the agent loop iteration.
	Iteration int `json:"iteration,omitempty"`
	// Arguments is the raw JSON arguments string.
	Arguments string `json:"arguments,omitempty"`
	// Result is the tool result sent back to the model.
	Result string `json:"result,omitempty"`
}

// Handoff is the payload of a handoff span.
type Handoff struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Context string `json:"context,omitempty"`
}

// MemoryOp is a memory operation.
type MemoryOp string

const (
	// MemoryRetrieve loads messages from memory.
	MemoryRetrieve MemoryOp = "retrieve"
	// MemorySave persists messages to memory.
	MemorySave MemoryOp = "save"
	// MemoryCompact replaces older messages of a running session with a
	// shorter version, such as a summary, to fit the model's context window.
	MemoryCompact MemoryOp = "compact"
	// MemorySnapshot stores a compaction's snapshot in a memory that keeps
	// snapshots, so later runs start from it.
	MemorySnapshot MemoryOp = "snapshot"
)

// Memory is the payload of a memory span.
type Memory struct {
	Op       MemoryOp      `json:"op"`
	Query    string        `json:"query,omitempty"`
	Messages []llm.Message `json:"messages,omitempty"`
	// Count is the number of messages loaded or saved.
	Count int `json:"count"`
}

// Hit is a single retrieval result.
type Hit struct {
	ID       string         `json:"id,omitempty"`
	Score    float64        `json:"score"`
	Content  string         `json:"content,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Retrieval is the payload of a semantic search span.
type Retrieval struct {
	Query  string `json:"query,omitempty"`
	TopK   int    `json:"topK"`
	Filter any    `json:"filter,omitempty"`
	Hits   []Hit  `json:"hits,omitempty"`
}

// Embedding is the payload of an embedding span.
type Embedding struct {
	Model  string    `json:"model,omitempty"`
	Inputs int       `json:"inputs"`
	Usage  llm.Usage `json:"usage"`
}

// Middleware is the payload of an LLM middleware span.
type Middleware struct {
	// Name is the middleware name: retry, limiter, cache or guardrail.
	Name string `json:"name"`
	// Attempt is the retry attempt number, starting at 1.
	Attempt int `json:"attempt,omitempty"`
	// Wait is the time spent waiting (limiter wait or retry backoff).
	Wait time.Duration `json:"wait,omitempty"`
	// Outcome is a short result such as "ok", "retry" or "blocked".
	Outcome string `json:"outcome,omitempty"`
	// Reason explains the outcome, such as the error that caused a retry.
	Reason string `json:"reason,omitempty"`
	// Rule names the rule involved, such as the guardrail name.
	Rule string `json:"rule,omitempty"`
}

// Custom is the payload of a user- or framework-defined span.
type Custom struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

func (*Run) kind() Kind        { return KindRun }
func (*LLMCall) kind() Kind    { return KindLLM }
func (*ToolCall) kind() Kind   { return KindTool }
func (*Handoff) kind() Kind    { return KindHandoff }
func (*Memory) kind() Kind     { return KindMemory }
func (*Retrieval) kind() Kind  { return KindRetrieval }
func (*Embedding) kind() Kind  { return KindEmbedding }
func (*Middleware) kind() Kind { return KindMiddleware }
func (*Custom) kind() Kind     { return KindCustom }

var (
	_ Payload = (*Run)(nil)
	_ Payload = (*LLMCall)(nil)
	_ Payload = (*ToolCall)(nil)
	_ Payload = (*Handoff)(nil)
	_ Payload = (*Memory)(nil)
	_ Payload = (*Retrieval)(nil)
	_ Payload = (*Embedding)(nil)
	_ Payload = (*Middleware)(nil)
	_ Payload = (*Custom)(nil)
)
