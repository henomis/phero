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
	"github.com/sashabaranov/go-openai"
)

// Message is an alias of the OpenAI chat completion message type.
type Message = openai.ChatCompletionMessage

// ToolCall is an alias of the OpenAI tool call type returned by the model.
type ToolCall = openai.ToolCall

// FunctionDefinition is an alias of the OpenAI function definition schema.
type FunctionDefinition = openai.FunctionDefinition

// ToolTypeFunction is the OpenAI tool type used for function tools.
const ToolTypeFunction = openai.ToolTypeFunction

// ChatMessageRoleSystem is the role for system messages.
const ChatMessageRoleSystem = openai.ChatMessageRoleSystem

// ChatMessageRoleUser is the role for user messages.
const ChatMessageRoleUser = openai.ChatMessageRoleUser

// ChatMessageRoleAssistant is the role for assistant messages.
const ChatMessageRoleAssistant = openai.ChatMessageRoleAssistant

// ChatMessageRoleTool is the role for tool-result messages.
const ChatMessageRoleTool = openai.ChatMessageRoleTool

// Result represents the output of an LLM execution, including the assistant message and any tool calls.
type Result struct {
	Message *Message
}

// LLM is the minimal interface implemented by chat-model backends.
//
// Implementations are expected to accept a list of messages and return the next
// assistant message. Tools can be configured via WithTools.
type LLM interface {
	Execute(context.Context, []Message, []*Tool) (*Result, error)
}

// ToolHandler is the low-level handler signature used by FunctionTool.
//
// arguments is the raw JSON string received from the model.
type ToolHandler func(ctx context.Context, arguments string) (any, error)

// Tool is a Tool that wraps a function.
type Tool struct {
	// The name of the tool, as shown to the LLM. Generally the name of the function.
	name string

	// A description of the tool, as shown to the LLM.
	description string

	// The JSON schema for the tool's parameters.
	inputSchema map[string]any

	// Handle calls the underlying function with the given arguments.
	handle ToolHandler
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
	return t.inputSchema
}

// Handle calls the underlying function with the given arguments.
func (t *Tool) Handle(ctx context.Context, arguments string) (any, error) {
	return t.handle(ctx, arguments)
}

// NewTool creates a new Tool with the given name, description, and handler function.
//
// The handler function must be of the form func(context.Context, T) (R, error) where T and R can be any types.
// The input type T is used to generate a JSON schema for the tool's parameters, which is passed to the LLM.
// When the tool is called, the LLM will provide the arguments as a JSON string, which will be unmarshaled into T and passed to the handler.
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
	reflector := &jsonschema.Reflector{
		ExpandedStruct:             true,
		RequiredFromJSONSchemaTags: false,
		AllowAdditionalProperties:  false,
	}

	var zero T
	t := reflect.TypeOf(zero)
	if t == nil {
		return nil, &ToolNilInputTypeError{ToolName: name}
	}

	schemaType := t
	schemaTarget := any(&zero)
	if t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		// If the handler takes a pointer-to-struct input (e.g. *Input), we still want
		// to generate a strict object schema based on the underlying struct (Input),
		// not a nullable pointer schema.
		schemaType = t.Elem()
		schemaTarget = reflect.New(schemaType).Interface()
	}
	var schema *jsonschema.Schema
	if schemaType.Kind() == reflect.Struct && schemaType.Name() == "" && schemaType.NumField() == 0 {
		// Avoid panic in jsonschema when reflecting an anonymous empty struct
		schema = &jsonschema.Schema{
			Version:    jsonschema.Version,
			Type:       "object",
			Properties: jsonschema.NewProperties(),
		}
		if !reflector.AllowAdditionalProperties {
			schema.AdditionalProperties = jsonschema.FalseSchema
		}
	} else {
		schema = reflector.Reflect(schemaTarget)
	}

	schemaMap, err := mapFromJSON(schema)
	if err != nil {
		return nil, &ToolSchemaTransformError{Err: err}
	}

	schemaMap, err = ensureStrictJSONSchema(schemaMap)
	if err != nil {
		return nil, &ToolSchemaStrictnessError{Err: err}
	}

	// Add description at the top level if provided
	if description != "" && schemaMap != nil {
		schemaMap["description"] = description
	}

	return &Tool{
		name:        name,
		description: description,
		inputSchema: schemaMap,
		handle: func(ctx context.Context, arguments string) (any, error) {
			var args T
			if err := json.Unmarshal([]byte(arguments), &args); err != nil {
				return nil, &ToolArgumentParseError{Err: err}
			}
			return handler(ctx, args)
		},
	}, nil
}
