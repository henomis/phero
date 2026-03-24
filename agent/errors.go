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
	// ErrMaxIterationsReached is returned when the agent loop reaches the maximum number of iterations.
	ErrMaxIterationsReached = errors.New("maximum iterations reached")
	// ErrSessionSaveFailed is returned when the memory save after a successful run fails.
	ErrSessionSaveFailed = errors.New("session save failed")
)

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
