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

package llm

import "context"

// Run identifies the agent run a tool call belongs to. The agent puts it in
// the context of every tool call, so a tool middleware can keep state for one
// run only, such as a permission a skill grants while the run lasts.
type Run struct {
	// ID identifies the run; it differs between runs.
	ID string

	afterEnd func(f func()) (stop func() bool)
}

// AfterEnd arranges for f to run in its own goroutine once the run ends, and
// returns a function that cancels that if it has not happened yet.
func (r Run) AfterEnd(f func()) (stop func() bool) {
	return r.afterEnd(f)
}

// runKey is the context key of the Run a tool call belongs to.
type runKey struct{}

// ContextWithRun returns a context carrying a Run with id, and the function
// that ends the run. The agent calls it at the start of each run and ends the
// run when the run returns.
func ContextWithRun(ctx context.Context, id string) (context.Context, context.CancelFunc) {
	runCtx, end := context.WithCancel(ctx)

	run := Run{ID: id, afterEnd: func(f func()) func() bool { return context.AfterFunc(runCtx, f) }}

	return context.WithValue(runCtx, runKey{}, run), end
}

// RunFromContext returns the Run ctx carries, set with ContextWithRun, and
// whether it carries one.
func RunFromContext(ctx context.Context) (Run, bool) {
	run, ok := ctx.Value(runKey{}).(Run)

	return run, ok
}
