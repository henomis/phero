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
	"time"

	"github.com/henomis/phero/llm"
)

// tracedLLM wraps an llm.LLM and emits LLMRequestEvent / LLMResponseEvent
// around every Execute call.
type tracedLLM struct {
	inner  llm.LLM
	tracer Tracer
}

// NewLLM wraps inner with tracing, emitting LLMRequestEvent before and
// LLMResponseEvent after each Execute call.
//
// When used inside an agent the AgentName and Iteration fields are populated
// automatically from the context. When used standalone they are left empty/zero.
func NewLLM(inner llm.LLM, t Tracer) llm.LLM {
	return &tracedLLM{inner: inner, tracer: t}
}

// Execute emits an LLMRequestEvent, delegates to the inner LLM, then emits an
// LLMResponseEvent.
func (tl *tracedLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	agentName := agentNameFromContext(ctx)
	iteration := iterationFromContext(ctx)

	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = t.Name()
	}

	tl.tracer.Trace(LLMRequestEvent{
		AgentName:    agentName,
		MessageCount: len(messages),
		ToolNames:    toolNames,
		Iteration:    iteration,
		Timestamp:    time.Now(),
	})

	result, err := tl.inner.Execute(ctx, messages, tools)

	var msg *llm.Message
	if result != nil {
		msg = result.Message
	}

	var usage *llm.Usage
	if result != nil {
		usage = result.Usage
	}

	tl.tracer.Trace(LLMResponseEvent{
		AgentName: agentName,
		Message:   msg,
		Usage:     usage,
		Iteration: iteration,
		Timestamp: time.Now(),
	})

	return result, err
}
