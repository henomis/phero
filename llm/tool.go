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
	"context"
	"encoding/json"
	"reflect"

	"github.com/invopop/jsonschema"
)

// ToolHandler is the low-level handler signature used by FunctionTool.
//
// arguments is the raw JSON string received from the model.
type ToolHandler func(ctx context.Context, arguments string) (any, error)

// ToolMiddleware wraps a tool handler.
//
// Middlewares can be used to add cross-cutting behavior (e.g. input validation,
// permission checks, logging) without baking that logic into each tool implementation.
//
// Middleware order is preserved: if you call tool.Use(m1, m2), m1 runs before m2.
type ToolMiddleware func(tool *Tool, next ToolHandler) ToolHandler

// toolCallKey is the context key of the tool call a handler runs for.
type toolCallKey struct{}

// ContextWithToolCall returns ctx carrying call, the model's tool call a tool
// handler is about to run for. The agent sets it before running each tool, so
// handlers and tool middlewares can read the call's ID and arguments with
// ToolCallFromContext.
func ContextWithToolCall(ctx context.Context, call ToolCall) context.Context {
	return context.WithValue(ctx, toolCallKey{}, call)
}

// ToolCallFromContext returns the tool call ctx carries, set with
// ContextWithToolCall, and whether it carries one.
func ToolCallFromContext(ctx context.Context) (ToolCall, bool) {
	call, ok := ctx.Value(toolCallKey{}).(ToolCall)

	return call, ok
}

// Tool is a Tool that wraps a function.
type Tool struct {
	// The name of the tool, as shown to the LLM. Generally the name of the function.
	name string

	// A description of the tool, as shown to the LLM.
	description string

	// The JSON schema for the tool's parameters, in the strict form, and as
	// reflected or given, where optional fields are not required.
	inputSchema map[string]any
	looseSchema map[string]any

	// strict reports whether InputSchema returns the strict schema.
	strict bool

	// Handle calls the underlying function with the given arguments.
	handle ToolHandler

	// middlewares wraps the tool handler to provide cross-cutting behavior
	// (e.g., input validation, permission checks, logging).
	middlewares []ToolMiddleware

	// exclusive marks a tool whose calls must not overlap any other call.
	exclusive bool
}

// SetStrict chooses the schema the tool is offered with. Strict (the default)
// lists every property as required and forbids others, as OpenAI and
// Anthropic strict tool use need, so the model always sends every field. Not
// strict keeps the schema as reflected (a field tagged omitempty is optional)
// or as given to NewRawTool, and providers send the tool without strict mode:
// useful for optional arguments, and for local servers that mishandle strict
// schemas.
func (t *Tool) SetStrict(strict bool) *Tool {
	t.strict = strict
	return t
}

// Strict reports whether the tool is offered in strict mode (see SetStrict).
func (t *Tool) Strict() bool {
	return t.strict
}

// Schema returns the tool's input schema in the strict form, or as reflected
// or given when strict is false. Providers that can turn strict mode off for
// every tool use it; InputSchema returns the tool's own choice.
func (t *Tool) Schema(strict bool) map[string]any {
	if strict || t.looseSchema == nil {
		return t.inputSchema
	}

	return t.looseSchema
}

// SetExclusive marks the tool as exclusive: when the model calls several
// tools at once, an agent runs each call of this tool alone, in call order,
// after the calls before it and before the calls after it. Use it for tools
// whose calls depend on each other, such as edits of the same file.
func (t *Tool) SetExclusive(exclusive bool) *Tool {
	t.exclusive = exclusive
	return t
}

// Exclusive reports whether the tool is exclusive (see SetExclusive).
func (t *Tool) Exclusive() bool {
	return t.exclusive
}

// Name returns the stable name used to identify this tool to the LLM.
func (t *Tool) Name() string {
	return t.name
}

// Description returns the description of the tool, as shown to the LLM.
func (t *Tool) Description() string {
	return t.description
}

// InputSchema returns the JSON schema for the tool's parameters, as shown to the LLM.
func (t *Tool) InputSchema() map[string]any {
	return t.Schema(t.strict)
}

// Handle calls the underlying function with the given arguments.
func (t *Tool) Handle(ctx context.Context, arguments string) (any, error) {
	h := t.handle
	for i := len(t.middlewares) - 1; i >= 0; i-- {
		h = t.middlewares[i](t, h)
	}

	return h(ctx, arguments)
}

// Use appends middleware(s) to this tool.
//
// Middlewares are executed in the order they are added.
func (t *Tool) Use(middlewares ...ToolMiddleware) *Tool {
	t.middlewares = append(t.middlewares, middlewares...)
	return t
}

// NewTool creates a new Tool with the given name, description, and handler function.
//
// The handler function must be of the form func(context.Context, T) (R, error) where T and R can be any types.
// The input type T is used to generate a JSON schema for the tool's parameters, which is passed to the LLM.
// When the tool is called, the LLM will provide the arguments as a JSON string, which will be
// unmarshaled into T and passed to the handler.
// The handler's return value R will be returned as the result of the tool call.
//
// Example usage:
//
//	type Input struct {
//		Text string `json:"text"`
//	}
//
//	type Output struct {
//		Reversed string `json:"reversed"`
//	}
//
//	func reverse(ctx context.Context, input Input) (Output, error) {
//		// reverse the input text and return it in Output.Reversed
//	}
//
//	reverseTool, err := NewTool(
//		"reverse",
//		"use this function to reverse a string",
//		reverse,
//	)
//	if err != nil {
//		// handle error
//	}
func NewTool[T, R any](name, description string, handler func(ctx context.Context, args T) (R, error)) (*Tool, error) {
	if name == "" {
		return nil, ErrToolNameRequired
	}

	schema, ok := reflectSchema[T]()
	if !ok {
		return nil, &ToolNilInputTypeError{ToolName: name}
	}

	schemaMap, err := mapFromJSON(schema)
	if err != nil {
		return nil, &ToolSchemaTransformError{Err: err}
	}

	strictSchema, looseSchema, err := strictAndLoose(schemaMap, description)
	if err != nil {
		return nil, err
	}

	return &Tool{
		name:        name,
		description: description,
		inputSchema: strictSchema,
		looseSchema: looseSchema,
		strict:      true,
		handle: func(ctx context.Context, arguments string) (any, error) {
			var args T
			if parseErr := json.Unmarshal([]byte(arguments), &args); parseErr != nil {
				return nil, &ToolArgumentParseError{Err: parseErr}
			}

			return handler(ctx, args)
		},
	}, nil
}

// reflectSchema builds the JSON schema of T. A pointer-to-struct is reflected
// as its struct, so the schema is a strict object rather than a nullable
// pointer. It reports false when T has a nil zero value (an interface type).
func reflectSchema[T any]() (*jsonschema.Schema, bool) {
	reflector := &jsonschema.Reflector{
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: false,
		AllowAdditionalProperties:  false,
	}

	var zero T

	t := reflect.TypeOf(zero)
	if t == nil {
		return nil, false
	}

	schemaType := t
	schemaTarget := any(&zero)

	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		schemaType = t.Elem()
		schemaTarget = reflect.New(schemaType).Interface()
	}

	if schemaType.Kind() == reflect.Struct && schemaType.Name() == "" && schemaType.NumField() == 0 {
		// Avoid panic in jsonschema when reflecting an anonymous empty struct
		schema := &jsonschema.Schema{
			Version:    jsonschema.Version,
			Type:       schemaTypeObject,
			Properties: jsonschema.NewProperties(),
		}
		if !reflector.AllowAdditionalProperties {
			schema.AdditionalProperties = jsonschema.FalseSchema
		}

		return schema, true
	}

	// ExpandedStruct inlines the root by looking its name up in the reflected
	// definitions, and jsonschema panics when there is no such entry — as for an
	// anonymous struct or a non-struct type. Those are inlined anyway.
	if schemaType.Kind() != reflect.Struct || schemaType.Name() == "" {
		reflector.ExpandedStruct = false
	}

	return reflector.Reflect(schemaTarget), true
}

// NewRawTool creates a new Tool from an externally supplied JSON Schema and a raw handler.
//
// Use this when the input schema is already known rather than inferred from a Go type —
// for example when forwarding tools from an MCP server or loading them from configuration.
//
// The provided schema is deep-cloned via a JSON round-trip and then normalized through
// ensureStrictJSONSchema, so the caller's original map is never mutated.
//
// The handler receives the raw JSON argument string exactly as sent by the model,
// without any intermediate unmarshaling.
func NewRawTool(name, description string, inputSchema map[string]any, handler ToolHandler) (*Tool, error) {
	if name == "" {
		return nil, ErrToolNameRequired
	}

	// Deep-clone via JSON round-trip so the caller's map is never mutated.
	schemaMap, err := jsonEncodeDecode[map[string]any](inputSchema)
	if err != nil {
		return nil, &ToolSchemaTransformError{Err: err}
	}

	strictSchema, looseSchema, err := strictAndLoose(schemaMap, description)
	if err != nil {
		return nil, err
	}

	return &Tool{
		name:        name,
		description: description,
		inputSchema: strictSchema,
		looseSchema: looseSchema,
		strict:      true,
		handle:      handler,
	}, nil
}

// strictAndLoose returns the strict form of schemaMap and a copy of it as it
// is, both with the tool's description at the top level when given. It may
// modify schemaMap.
func strictAndLoose(schemaMap map[string]any, description string) (map[string]any, map[string]any, error) {
	looseSchema, err := jsonEncodeDecode[map[string]any](schemaMap)
	if err != nil {
		return nil, nil, &ToolSchemaTransformError{Err: err}
	}

	// An empty schema has no loose form worth keeping: the strict empty
	// object is valid either way.
	if len(looseSchema) == 0 {
		looseSchema = nil
	}

	strictSchema, err := ensureStrictJSONSchema(schemaMap)
	if err != nil {
		return nil, nil, &ToolSchemaStrictnessError{Err: err}
	}

	if description != "" {
		if strictSchema != nil {
			strictSchema["description"] = description
		}

		if looseSchema != nil {
			looseSchema["description"] = description
		}
	}

	return strictSchema, looseSchema, nil
}
