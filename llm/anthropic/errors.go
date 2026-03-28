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

package anthropic

import (
	"errors"
	"fmt"
)

// ErrToolMessageMissingToolCallID is returned when an OpenAI-shaped tool message
// is missing the ToolCallID field, which Anthropic requires as tool_use_id.
var ErrToolMessageMissingToolCallID = errors.New("anthropic: tool message missing tool_call_id")

// UnsupportedRoleError is returned when converting OpenAI-shaped messages that
// contain a role unsupported by Anthropic's Messages API.
//
// Supported roles at the boundary are: system/user/assistant/tool.
type UnsupportedRoleError struct {
	Role string
}

func (e *UnsupportedRoleError) Error() string {
	return fmt.Sprintf("anthropic: unsupported role %q", e.Role)
}

// NilResponseError is returned when the Anthropic SDK returns a nil message.
type NilResponseError struct{}

func (e *NilResponseError) Error() string {
	return "anthropic: nil response"
}

// ToolArgumentsParseError is returned when a tool call's Arguments field is not
// valid JSON, which is required to construct an Anthropic tool_use block.
type ToolArgumentsParseError struct {
	ToolName string
	Err      error
}

func (e *ToolArgumentsParseError) Error() string {
	if e.ToolName == "" {
		return fmt.Sprintf("anthropic: invalid tool arguments: %v", e.Err)
	}
	return fmt.Sprintf("anthropic: invalid tool arguments for %q: %v", e.ToolName, e.Err)
}

func (e *ToolArgumentsParseError) Unwrap() error {
	return e.Err
}
