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

import (
	"errors"
	"fmt"
	"time"
)

// ErrSchemaAdditionalPropertiesSet is returned when an object schema explicitly
// sets additionalProperties in a way that is incompatible with strict schemas.
var ErrSchemaAdditionalPropertiesSet = errors.New(
	"additionalProperties should not be set for object types. " +
		"This could be because you configured additional properties to be allowed. " +
		"If you really need this, update the function or output tool to not use a strict schema",
)

// SchemaExpectedMapError is returned when schema normalization expects a JSON
// object (map[string]any) but receives a different type.
type SchemaExpectedMapError struct {
	Got  any
	Path []string
}

func (e *SchemaExpectedMapError) Error() string {
	return fmt.Sprintf("expected %#v to be a map[string]any, path=%+v", e.Got, e.Path)
}

// SchemaNonStringRefError is returned when a schema contains a $ref value that
// is not a string.
type SchemaNonStringRefError struct {
	RawRef any
}

func (e *SchemaNonStringRefError) Error() string {
	return fmt.Sprintf("received non-string $ref: %#v", e.RawRef)
}

// SchemaUnexpectedRefFormatError is returned when a schema $ref does not use a
// supported format.
type SchemaUnexpectedRefFormatError struct {
	Ref string
}

func (e *SchemaUnexpectedRefFormatError) Error() string {
	return fmt.Sprintf("unexpected $ref format: expected `#/` prefix in $ref value %q", e.Ref)
}

// SchemaNonDictionaryWhileResolvingRefError is returned when resolving a $ref
// encounters a non-map value while walking the path.
type SchemaNonDictionaryWhileResolvingRefError struct {
	Ref      string
	Resolved any
}

func (e *SchemaNonDictionaryWhileResolvingRefError) Error() string {
	return fmt.Sprintf("encountered non-dictionary entry while resolving $ref %q: %#v", e.Ref, e.Resolved)
}

// ToolNilInputTypeError is returned when a tool's input type parameter has a nil zero value.
type ToolNilInputTypeError struct {
	ToolName string
}

func (e *ToolNilInputTypeError) Error() string {
	return fmt.Sprintf("failed to infer tool input type for %q: type parameter has nil zero value", e.ToolName)
}

// ErrToolNameRequired is returned by NewTool when the tool name is empty.
var ErrToolNameRequired = errors.New("tool name is required")

// ToolSchemaTransformError is returned when transforming a jsonschema.Schema to a map fails.
type ToolSchemaTransformError struct {
	Err error
}

func (e *ToolSchemaTransformError) Error() string {
	return fmt.Sprintf("failed to transform function tool jsonschema.Schema to map: %v", e.Err)
}

func (e *ToolSchemaTransformError) Unwrap() error {
	return e.Err
}

// ToolSchemaStrictnessError is returned when ensuring strictness of a tool's JSON schema fails.
type ToolSchemaStrictnessError struct {
	Err error
}

func (e *ToolSchemaStrictnessError) Error() string {
	return fmt.Sprintf("failed to ensure strictness of function tool json schema: %v", e.Err)
}

func (e *ToolSchemaStrictnessError) Unwrap() error {
	return e.Err
}

// ToolArgumentParseError is returned when parsing tool arguments from JSON fails.
type ToolArgumentParseError struct {
	Err error
}

func (e *ToolArgumentParseError) Error() string {
	return fmt.Sprintf("failed to parse arguments: %v", e.Err)
}

func (e *ToolArgumentParseError) Unwrap() error {
	return e.Err
}

// ErrInvalidToolChoice is returned when a call's tool choice is incoherent: an
// unknown mode, a forced tool that is not offered, or a tool requirement on a
// call that offers no tools.
var ErrInvalidToolChoice = errors.New("invalid tool choice")

// ErrInvalidResponseFormat is returned when a response format has no name or
// no schema.
var ErrInvalidResponseFormat = errors.New("invalid response format: name and schema are required")

// ErrUnsupportedCallOption is returned by a backend that cannot honor a call
// option it was given, rather than silently ignoring it.
var ErrUnsupportedCallOption = errors.New("call option not supported by this LLM")

// ResponseFormatSchemaError is returned when building a response format's JSON
// schema fails.
type ResponseFormatSchemaError struct {
	Name string
	Err  error
}

func (e *ResponseFormatSchemaError) Error() string {
	return fmt.Sprintf("failed to build response format %q schema: %v", e.Name, e.Err)
}

func (e *ResponseFormatSchemaError) Unwrap() error {
	return e.Err
}

// ProviderError is an error answered by an LLM provider's API with an HTTP
// status, in a form that does not depend on the provider's SDK. Provider
// packages (llm/openai, llm/anthropic) wrap such errors in it, so code that is
// not allowed to import an SDK — a retry policy, a server choosing the error
// code it sends — can still tell a rate limit from a bad API key with
// errors.As.
//
// Error returns the wrapped error's text unchanged.
type ProviderError struct {
	// Provider names the provider package, such as "openai" or "anthropic".
	Provider string
	// StatusCode is the HTTP status the API answered with.
	StatusCode int
	// RetryAfter is how long the provider asked the caller to wait, typically
	// with a 429; 0 when it did not say or the SDK does not expose it.
	RetryAfter time.Duration
	// Err is the SDK's own error.
	Err error
}

func (e *ProviderError) Error() string {
	return e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	return e.Err
}
