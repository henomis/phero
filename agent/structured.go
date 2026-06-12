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
	"context"

	"github.com/henomis/phero/llm"
)

// finalAnswerToolName is the name of the synthetic tool used to capture a typed
// final answer in RunTyped.
const finalAnswerToolName = "final_answer"

// structuredInstruction is appended to the agent description during RunTyped to
// steer the model toward producing its result via the final_answer tool.
const structuredInstruction = "When you have completed the task, you MUST call the `" +
	finalAnswerToolName + "` tool exactly once with your complete, structured result. " +
	"Do not write the final answer as plain text."

// RunTyped runs the agent and returns its final answer decoded into T.
//
// It works on any llm.LLM backend without provider-specific structured-output
// support: a synthetic final_answer tool is added to the agent whose JSON schema
// is derived from T (via llm.NewTool), and the agent is instructed to call it to
// finish. The tool call's arguments are decoded into T.
//
// T should be a struct so that a top-level object schema is generated (the same
// requirement as tool input types). The original agent is not mutated; RunTyped
// runs against a shallow copy that shares the LLM, memory, tracer, tools, and
// handoffs.
//
// The agent's *Result is returned alongside T (and may be non-nil even on error).
// If the agent finishes without ever calling final_answer, the returned error is
// ErrNoStructuredOutput.
func RunTyped[T any](ctx context.Context, a *Agent, parts ...llm.ContentPart) (T, *Result, error) {
	var (
		zero       T
		captured   T
		didCapture bool
	)

	if _, exists := a.getTool(finalAnswerToolName); exists {
		return zero, nil, &ToolAlreadyExistsError{Name: finalAnswerToolName}
	}

	finalTool, err := llm.NewTool(
		finalAnswerToolName,
		"Record your complete, final answer. Call this exactly once to finish.",
		func(_ context.Context, args T) (string, error) {
			captured = args
			didCapture = true
			return "Final answer recorded.", nil
		},
	)
	if err != nil {
		return zero, nil, err
	}

	result, err := a.cloneWith(structuredInstruction, finalTool).Run(ctx, parts...)
	if err != nil {
		return zero, result, err
	}
	if !didCapture {
		return zero, result, ErrNoStructuredOutput
	}
	return captured, result, nil
}

// cloneWith returns a shallow copy of the agent with extraInstruction appended to
// its description and extraTools appended to its tool list. The copy shares the
// LLM, memory, tracer, and handoff map with the original, which is left unchanged.
func (a *Agent) cloneWith(extraInstruction string, extraTools ...*llm.Tool) *Agent {
	tools := make([]*llm.Tool, 0, len(a.tools)+len(extraTools))
	tools = append(tools, a.tools...)
	tools = append(tools, extraTools...)

	description := a.description
	if extraInstruction != "" {
		description = a.description + "\n\n" + extraInstruction
	}

	return &Agent{
		llm:           a.llm,
		name:          a.name,
		description:   description,
		maxIterations: a.maxIterations,
		tools:         tools,
		memory:        a.memory,
		tracer:        a.tracer,
		handoffs:      a.handoffs,
	}
}
