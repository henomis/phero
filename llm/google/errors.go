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

package google

import (
	"errors"
	"fmt"
)

// ErrToolMessageMissingToolCallID is returned when a tool message has no
// ToolCallID, which is needed to find the function call it answers.
var ErrToolMessageMissingToolCallID = errors.New("google: tool message missing tool_call_id")

// ErrImageURLUnsupported is returned for an image given by URL: the Gemini API
// reads only inline data or files uploaded to it. Use llm.ImageBase64 or
// llm.ImageFile instead.
var ErrImageURLUnsupported = errors.New("google: image URLs are not supported, use llm.ImageBase64 or llm.ImageFile")

// ClientInitError is returned by every call when the SDK client could not be
// created, for instance without an API key.
type ClientInitError struct {
	Err error
}

func (e *ClientInitError) Error() string {
	return fmt.Sprintf("google: create client: %v", e.Err)
}

func (e *ClientInitError) Unwrap() error {
	return e.Err
}

// UnsupportedRoleError is returned for a message whose role is not one of
// system/user/assistant/tool.
type UnsupportedRoleError struct {
	Role string
}

func (e *UnsupportedRoleError) Error() string {
	return fmt.Sprintf("google: unsupported role %q", e.Role)
}

// UnknownToolCallError is returned for a tool message whose ToolCallID matches
// no function call of an earlier assistant message: Gemini needs the
// function's name to accept the result.
type UnknownToolCallError struct {
	ToolCallID string
}

func (e *UnknownToolCallError) Error() string {
	return fmt.Sprintf("google: tool result %q answers no earlier function call", e.ToolCallID)
}

// ToolArgumentsParseError is returned when a tool call's Arguments field is not
// a JSON object, which Gemini requires as the function call's args.
//
// Deprecated: stored tool calls with invalid arguments are now sent with an
// empty object instead, so this error is no longer returned.
type ToolArgumentsParseError struct {
	ToolName string
	Err      error
}

func (e *ToolArgumentsParseError) Error() string {
	return fmt.Sprintf("google: invalid tool arguments for %q: %v", e.ToolName, e.Err)
}

func (e *ToolArgumentsParseError) Unwrap() error {
	return e.Err
}

// InvalidImageError is returned for an inline image whose data is not valid
// base64.
type InvalidImageError struct {
	Err error
}

func (e *InvalidImageError) Error() string {
	return fmt.Sprintf("google: invalid base64 image: %v", e.Err)
}

func (e *InvalidImageError) Unwrap() error {
	return e.Err
}

// NoContentError is returned when Gemini answers without any content, for
// instance when the prompt or the answer was blocked.
type NoContentError struct {
	// FinishReason is why the candidate stopped, when there was one.
	FinishReason string
	// BlockReason is why the prompt was blocked, when it was.
	BlockReason string
}

func (e *NoContentError) Error() string {
	switch {
	case e.BlockReason != "":
		return fmt.Sprintf("google: prompt blocked: %s", e.BlockReason)
	case e.FinishReason != "":
		return fmt.Sprintf("google: empty answer (finish reason %s)", e.FinishReason)
	default:
		return "google: empty answer"
	}
}

// IncompleteStreamError is returned when a streamed response ends before any
// part arrived, leaving no message to return.
type IncompleteStreamError struct{}

func (e *IncompleteStreamError) Error() string {
	return "google: stream ended without a complete message"
}
