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

package middleware

import (
	"context"
	"fmt"
	"iter"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
)

const (
	stageInput  = "input"
	stageOutput = "output"
)

// MessageGuard checks the outbound message list before it is sent to the model.
// Returning a non-nil error blocks execution.
type MessageGuard func(ctx context.Context, messages []llm.Message) error

// ResultGuard checks the model result after the call completes successfully.
// Returning a non-nil error rejects the result and returns that error to the caller.
type ResultGuard func(ctx context.Context, result *llm.Result) error

// GuardrailError wraps a named guard failure.
type GuardrailError struct {
	// Stage identifies whether the failure happened on input or output.
	Stage string
	// Name is the guard name provided at middleware construction time.
	Name string
	// Err is the underlying guard error.
	Err error
}

// Error implements the error interface.
func (e *GuardrailError) Error() string {
	return fmt.Sprintf("middleware: %s guard %q failed: %v", e.Stage, e.Name, e.Err)
}

// Unwrap returns the underlying guard error.
func (e *GuardrailError) Unwrap() error {
	return e.Err
}

// Is reports true for llm.ErrGuardrailBlocked, so callers can detect a
// guardrail block without importing this package.
func (e *GuardrailError) Is(target error) bool {
	return target == llm.ErrGuardrailBlocked
}

// GuardrailOption configures a Guardrails middleware.
type GuardrailOption func(*guardrailConfig)

type namedMessageGuard struct {
	name  string
	guard MessageGuard
}

type namedResultGuard struct {
	name  string
	guard ResultGuard
}

type guardrailConfig struct {
	messageGuards []namedMessageGuard
	resultGuards  []namedResultGuard
}

// WithMessageGuard adds a named input guard that inspects messages before the
// model call is executed.
func WithMessageGuard(name string, guard MessageGuard) GuardrailOption {
	return func(cfg *guardrailConfig) {
		cfg.messageGuards = append(cfg.messageGuards, namedMessageGuard{name: name, guard: guard})
	}
}

// WithResultGuard adds a named output guard that inspects the result after the
// model call completes successfully.
func WithResultGuard(name string, guard ResultGuard) GuardrailOption {
	return func(cfg *guardrailConfig) {
		cfg.resultGuards = append(cfg.resultGuards, namedResultGuard{name: name, guard: guard})
	}
}

// NewGuardrails returns an llm.Middleware that executes message guards
// before the model call and result guards after a successful response.
//
// Guards are executed in the order they are added. The first failing guard stops
// the request and its error is wrapped in GuardrailError.
//
//	mw := middleware.NewGuardrails(
//		middleware.WithMessageGuard("prompt-size", func(ctx context.Context, messages []llm.Message) error {
//			return nil
//		}),
//	)
func NewGuardrails(opts ...GuardrailOption) llm.Middleware {
	cfg := &guardrailConfig{}
	for _, o := range opts {
		o(cfg)
	}

	return func(next llm.LLM) llm.LLM {
		return &guardrailsLLM{inner: next, cfg: cfg}
	}
}

// guardrailsLLM is the concrete LLM produced by the Guardrails middleware.
type guardrailsLLM struct {
	inner llm.LLM
	cfg   *guardrailConfig
}

// Execute validates input messages, delegates to inner, and validates the result.
func (g *guardrailsLLM) Execute(
	ctx context.Context, messages []llm.Message, opts ...llm.CallOption,
) (*llm.Result, error) {
	if err := g.checkInput(ctx, messages); err != nil {
		return nil, err
	}

	result, err := g.inner.Execute(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}

	if guardErr := g.checkResult(ctx, result); guardErr != nil {
		return nil, guardErr
	}

	return result, nil
}

// ExecuteStream validates input messages before the stream starts, and the
// assembled result when the terminal chunk arrives. The deltas before it have
// already reached the caller: a result guard that fails replaces the terminal
// chunk with its GuardrailError, so the caller never gets the result.
func (g *guardrailsLLM) ExecuteStream(
	ctx context.Context, messages []llm.Message, opts ...llm.CallOption,
) iter.Seq2[llm.StreamChunk, error] {
	if err := g.checkInput(ctx, messages); err != nil {
		return errorStream(err)
	}

	return func(yield func(llm.StreamChunk, error) bool) {
		for chunk, err := range llm.StreamOrBuffer(ctx, g.inner, messages, opts...) {
			if err == nil && chunk.Done {
				if guardErr := g.checkResult(ctx, resultFromChunk(chunk)); guardErr != nil {
					yield(llm.StreamChunk{}, guardErr)
					return
				}
			}

			if !yield(chunk, err) || err != nil {
				return
			}
		}
	}
}

// checkInput runs the message guards in order and returns the first failure.
func (g *guardrailsLLM) checkInput(ctx context.Context, messages []llm.Message) error {
	for _, guard := range g.cfg.messageGuards {
		if err := guard.guard(ctx, messages); err != nil {
			return blocked(ctx, &GuardrailError{Stage: stageInput, Name: guard.name, Err: err})
		}
	}

	return nil
}

// checkResult runs the result guards in order and returns the first failure.
func (g *guardrailsLLM) checkResult(ctx context.Context, result *llm.Result) error {
	for _, guard := range g.cfg.resultGuards {
		if err := guard.guard(ctx, result); err != nil {
			return blocked(ctx, &GuardrailError{Stage: stageOutput, Name: guard.name, Err: err})
		}
	}

	return nil
}

// blocked records a guardrail.blocked event on the current span and returns e.
func blocked(ctx context.Context, e *GuardrailError) error {
	trace.SpanFrom(ctx).AddEvent(eventGuardrailBlocked,
		trace.String(attrGuardrailStage, e.Stage),
		trace.String(attrGuardrailName, e.Name),
		trace.String(attrGuardrailError, e.Err.Error()),
	)

	return e
}
