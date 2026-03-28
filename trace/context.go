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

import "context"

type contextKey int

const (
	tracerKey    contextKey = iota
	agentNameKey contextKey = iota
	iterationKey contextKey = iota
)

// WithTracer returns a new context carrying the given Tracer.
//
// Use FromContext to retrieve it in tool handlers or other downstream code.
func WithTracer(ctx context.Context, t Tracer) context.Context {
	return context.WithValue(ctx, tracerKey, t)
}

// FromContext returns the Tracer stored in ctx.
//
// If no tracer was set, Noop is returned so callers never receive a nil Tracer.
func FromContext(ctx context.Context) Tracer {
	t, ok := ctx.Value(tracerKey).(Tracer)
	if !ok || t == nil {
		return Noop
	}
	return t
}

// WithAgentName returns a context carrying the given agent name.
//
// The agent uses this internally so that events emitted by the LLM wrapper are
// attributed to the correct agent even when the wrapper is called standalone.
func WithAgentName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, agentNameKey, name)
}

// agentNameFromContext returns the agent name stored in ctx, or an empty string.
func agentNameFromContext(ctx context.Context) string {
	name, _ := ctx.Value(agentNameKey).(string)
	return name
}

// WithIteration returns a context carrying the current agent loop iteration number.
//
// The LLM wrapper reads this to annotate LLMRequestEvent and LLMResponseEvent
// with the correct iteration.
func WithIteration(ctx context.Context, iteration int) context.Context {
	return context.WithValue(ctx, iterationKey, iteration)
}

// iterationFromContext returns the iteration number stored in ctx, or zero.
func iterationFromContext(ctx context.Context) int {
	n, _ := ctx.Value(iterationKey).(int)
	return n
}
