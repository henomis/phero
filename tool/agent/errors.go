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

import "errors"

var (
	// ErrLLMRequired is returned when creating a tool with a nil LLM.
	ErrLLMRequired = errors.New("agent tool: llm client is required")
	// ErrNameRequired is returned when the agent name field in the tool input is empty.
	ErrNameRequired = errors.New("agent tool: name is required")
	// ErrDescriptionRequired is returned when the agent description field in the tool input is empty.
	ErrDescriptionRequired = errors.New("agent tool: description is required")
	// ErrNilInput is returned when the tool handler receives a nil input object.
	ErrNilInput = errors.New("agent tool: nil input")
	// ErrInputRequired is returned when the input instructions are empty.
	ErrInputRequired = errors.New("agent tool: input is required")
)
