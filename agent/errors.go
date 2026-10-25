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
	"errors"
	"fmt"
)

var (
	// ErrUndefinedLLM is returned when creating an agent with a nil LLM client.
	ErrUndefinedLLM = errors.New("undefined LLM client")
	// ErrDescriptionRequired is returned when creating an agent with an empty description.
	ErrDescriptionRequired = errors.New("agent description is required")
	// ErrNameRequired is returned when creating an agent with an empty name.
	ErrNameRequired = errors.New("agent name is required")
	// ErrMaxIterationsReached is returned when the agent loop reaches the maximum number of
	// iterations. Run() will still return any partial text result alongside this error.
	ErrMaxIterationsReached = errors.New("maximum iterations reached")
	// ErrSessionSaveFailed is returned when the memory save after a successful run fails.
	ErrSessionSaveFailed = errors.New("session save failed")
	// ErrStopRun matches the error of a run that a tool stopped with StopRun.
	ErrStopRun = errors.New("run stopped by a tool")
	// ErrIncompleteStream is returned by RunStream when the LLM stream ended without
	// a terminal chunk carrying the assembled message.
	ErrIncompleteStream = errors.New("llm stream ended without a complete message")
	// ErrInvalidCompaction is returned when a Compactor's result does not
	// keep the session's last messages as they were (see Compaction.Kept).
	ErrInvalidCompaction = errors.New("invalid compaction")
	// ErrSnapshotMismatch is recorded when a compaction's snapshot cannot be
	// stored because the compacted messages are not found in the memory's
	// log; the snapshot then lives in the run only.
	ErrSnapshotMismatch = errors.New("compacted messages not found in the memory log")
)

// StopRun returns the error a tool's handler returns to end the agent's run
// on purpose, for example to wait for a human: the run ends once every tool
// call of the current round has run, without another LLM call. Run returns
// the partial result, with its Summary (StopReason trace.StopRequested), and
// an error matching ErrStopRun and cause. The run's span ends without an
// error: a requested stop is not a failure.
//
// The tool's call is recorded as successful, with "The run was stopped:"
// and the error's text as its result. A sub-agent run through AsTool that is
// stopped stops its caller's run too, since AsTool returns its error.
// Cancelling the run's context with a StopRun cause
// (context.WithCancelCause) is reported the same way.
func StopRun(cause error) error {
	if cause == nil {
		return ErrStopRun
	}

	return fmt.Errorf("%w: %w", ErrStopRun, cause)
}

// ToolAlreadyExistsError is returned when a tool with the same name is already registered.
type ToolAlreadyExistsError struct {
	Name string
}

func (e *ToolAlreadyExistsError) Error() string {
	return fmt.Sprintf("tool with name %q already exists", e.Name)
}

// ToolUnknownError is returned when a tool with the specified name is not found.
type ToolUnknownError struct {
	Name string
}

func (e *ToolUnknownError) Error() string {
	return fmt.Sprintf("tool with name %q not found", e.Name)
}

// ToolExecutionError is returned when an error occurs during the execution of a tool.
type ToolExecutionError struct {
	Name string
	Err  error
}

func (e *ToolExecutionError) Error() string {
	return fmt.Sprintf("error executing tool %s: %v", e.Name, e.Err)
}

func (e *ToolExecutionError) Unwrap() error {
	return e.Err
}

// ToolPanicError is the error of a tool whose handler panicked. The agent
// recovers the panic and reports it to the model as a failed tool call, so
// one faulty tool does not crash the process.
type ToolPanicError struct {
	Name  string
	Value any
}

func (e *ToolPanicError) Error() string {
	return fmt.Sprintf("tool %s panicked: %v", e.Name, e.Value)
}

// CompactionError is returned when compacting a running session fails, for
// example because the summary model's call failed.
type CompactionError struct {
	Err error
}

func (e *CompactionError) Error() string {
	return fmt.Sprintf("session compaction failed: %v", e.Err)
}

func (e *CompactionError) Unwrap() error {
	return e.Err
}
