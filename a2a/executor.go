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

package a2a

import (
	"context"
	"errors"
	"iter"
	"sync"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	"github.com/henomis/phero/agent"
)

// agentExecutor bridges the a2asrv.AgentExecutor interface to a phero agent.
//
// It tracks in-flight executions via a sync.Map so that Cancel() can interrupt
// them through context cancellation rather than merely reporting a status update.
type agentExecutor struct {
	agent   *agent.Agent
	cancels sync.Map // a2a.TaskID → context.CancelFunc
}

// Execute implements a2asrv.AgentExecutor.
//
// Event sequence emitted to the framework:
//  1. Submitted (only for new tasks without a stored state)
//  2. Working
//  3. Completed (with the translated result) or Failed (with error text)
//     or Canceled (when cancelled by a concurrent Cancel call or a request deadline)
//
// Incoming A2A message parts are translated to phero ContentParts via
// translatePartsToPhero. The agent result is translated back via translateResultToA2A.
func (e *agentExecutor) Execute(ctx context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[sdka2a.Event, error] {
	return func(yield func(sdka2a.Event, error) bool) {
		// Announce the task as submitted when it is new.
		if execCtx.StoredTask == nil {
			if !yield(sdka2a.NewSubmittedTask(execCtx, execCtx.Message), nil) {
				return
			}
		}

		// Signal working.
		if !yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateWorking, nil), nil) {
			return
		}

		// Register a cancellable context so a concurrent Cancel() call can interrupt Run.
		taskCtx, cancel := context.WithCancel(ctx)

		e.cancels.Store(execCtx.TaskID, cancel)
		defer func() {
			cancel()
			e.cancels.Delete(execCtx.TaskID)
		}()

		parts := translatePartsToPhero(execCtx.Message)

		result, err := e.agent.Run(taskCtx, parts...)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateCanceled, nil), nil)
				return
			}

			errMsg := sdka2a.NewMessageForTask(sdka2a.MessageRoleAgent, execCtx, sdka2a.NewTextPart(err.Error()))
			yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateFailed, errMsg), nil)

			return
		}

		a2aParts := translateResultToA2A(result)
		if len(a2aParts) == 0 {
			// Fallback: use text content from result if part translation produced nothing.
			a2aParts = []*sdka2a.Part{sdka2a.NewTextPart(result.TextContent())}
		}

		responseMsg := sdka2a.NewMessageForTask(sdka2a.MessageRoleAgent, execCtx, a2aParts...)
		yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateCompleted, responseMsg), nil)
	}
}

// Cancel implements a2asrv.AgentExecutor.
//
// It looks up the in-flight cancel function for the task and calls it, which
// causes the running agent.Run to return with context.Canceled. The framework
// will then pick up the Canceled status emitted by Execute.
func (e *agentExecutor) Cancel(_ context.Context, execCtx *a2asrv.ExecutorContext) iter.Seq2[sdka2a.Event, error] {
	return func(yield func(sdka2a.Event, error) bool) {
		if fn, ok := e.cancels.Load(execCtx.TaskID); ok {
			if cancelFn, ok := fn.(context.CancelFunc); ok {
				cancelFn()
			}
		}

		yield(sdka2a.NewStatusUpdateEvent(execCtx, sdka2a.TaskStateCanceled, nil), nil)
	}
}
