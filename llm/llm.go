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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/invopop/jsonschema"
)

// ContentType identifies the kind of content within a ContentPart.
type ContentType string

const (
	// ContentTypeText is a plain-text content part.
	ContentTypeText ContentType = "text"
	// ContentTypeImageURL is an image referenced by URL.
	ContentTypeImageURL ContentType = "image_url"
	// ContentTypeImageBase64 is an image provided as raw base64-encoded bytes.
	// Use ImageBase64 or ImageFile to create parts of this type.
	ContentTypeImageBase64 ContentType = "image_base64"
)

// ContentPart is a single piece of content within a message — either text or an image.
type ContentPart struct {
	// Type identifies the kind of content.
	Type ContentType
	// Text holds the text content when Type is ContentTypeText.
	Text string
	// ImageURL holds the image URL when Type is ContentTypeImageURL.
	ImageURL string
	// ImageBase64 holds the base64-encoded image bytes when Type is ContentTypeImageBase64.
	ImageBase64 string
	// MIMEType is the MIME type of the image (e.g. "image/png") when Type is ContentTypeImageBase64.
	MIMEType string
}

// Text returns a ContentPart containing the given plain text.
func Text(s string) ContentPart {
	return ContentPart{Type: ContentTypeText, Text: s}
}

// ImageURL returns a ContentPart referencing an image at the given URL.
func ImageURL(url string) ContentPart {
	return ContentPart{Type: ContentTypeImageURL, ImageURL: url}
}

// ImageBase64 returns a ContentPart carrying a base64-encoded image.
//
// mimeType must be one of: "image/jpeg", "image/png", "image/gif", "image/webp".
// data must be the standard base64 encoding of the raw image bytes.
func ImageBase64(mimeType, data string) ContentPart {
	return ContentPart{Type: ContentTypeImageBase64, MIMEType: mimeType, ImageBase64: data}
}

// ImageFile reads the image at path and returns a ContentPart with its
// base64-encoded content.
//
// The MIME type is detected from the file contents (falling back to the file
// extension when detection is ambiguous). Supported types are the same as
// those accepted by OpenAI and Anthropic: image/jpeg, image/png, image/gif,
// and image/webp.
func ImageFile(path string) (ContentPart, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ContentPart{}, fmt.Errorf("llm.ImageFile: read %q: %w", path, err)
	}

	mimeType := detectImageMIMEType(data, path)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		// accepted
	default:
		return ContentPart{}, fmt.Errorf("llm.ImageFile: unsupported image type %q for %q", mimeType, path)
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	return ImageBase64(mimeType, b64), nil
}

// detectImageMIMEType sniffs the MIME type from the first 512 bytes, then
// falls back to the file extension.
func detectImageMIMEType(data []byte, path string) string {
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	mt := http.DetectContentType(sniff)
	// http.DetectContentType returns "image/jpeg", "image/png", "image/gif",
	// "image/webp" for the respective formats.
	if strings.HasPrefix(mt, "image/") {
		return mt
	}
	// Fall back to extension.
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return mt
}

// FunctionCall holds the name and JSON-encoded arguments of a tool invocation.
type FunctionCall struct {
	// Name is the name of the tool being called.
	Name string
	// Arguments is the raw JSON-encoded argument object.
	Arguments string
}

// ToolCall represents a single tool invocation requested by the model.
type ToolCall struct {
	// ID is the opaque identifier assigned by the model to correlate this call with its result.
	ID string
	// Type is the tool type (always ToolTypeFunction for function tools).
	Type string
	// Function holds the name and arguments for this call.
	Function FunctionCall
}

// Message is a single turn in a conversation between a user, assistant, or tool.
type Message struct {
	// Role is the participant role: one of RoleSystem, RoleUser, RoleAssistant, RoleTool.
	Role string
	// Parts holds the multimodal content of the message.
	// For simple text messages this will be a single ContentPart with Type ContentTypeText.
	Parts []ContentPart
	// ToolCalls holds tool invocations requested by the assistant.
	// Only populated on assistant messages.
	ToolCalls []ToolCall
	// ToolCallID is the ID of the ToolCall this message is a response to.
	// Only set on tool-result messages (Role == RoleTool).
	ToolCallID string
	// Name is an optional participant name, used by some providers.
	Name string
}

// TextContent returns the concatenation of all text parts in the message.
func (m Message) TextContent() string {
	return TextContent(m.Parts...)
}

// TextContent returns the concatenation of all text parts across the given ContentParts.
func TextContent(parts ...ContentPart) string {
	var sb strings.Builder
	for i, p := range parts {
		if p.Type == ContentTypeText {
			if i > 0 && sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// Message role constants.
const (
	// RoleSystem is the role for system (instruction) messages.
	RoleSystem = "system"
	// RoleUser is the role for user messages.
	RoleUser = "user"
	// RoleAssistant is the role for assistant (model) messages.
	RoleAssistant = "assistant"
	// RoleTool is the role for tool-result messages.
	RoleTool = "tool"
	// ToolTypeFunction is the tool type string for function-style tools.
	ToolTypeFunction = "function"
)

// SystemMessage constructs a system-role Message containing a single text part.
func SystemMessage(text string) Message {
	return Message{Role: RoleSystem, Parts: []ContentPart{Text(text)}}
}

// UserMessage constructs a user-role Message from the given content parts.
func UserMessage(parts ...ContentPart) Message {
	return Message{Role: RoleUser, Parts: parts}
}

// AssistantMessage constructs an assistant-role Message from the given parts and optional tool calls.
func AssistantMessage(parts []ContentPart, toolCalls ...ToolCall) Message {
	return Message{Role: RoleAssistant, Parts: parts, ToolCalls: toolCalls}
}

// ToolResultMessage constructs a tool-role Message carrying the result of a tool call.
func ToolResultMessage(toolCallID string, parts ...ContentPart) Message {
	return Message{Role: RoleTool, ToolCallID: toolCallID, Parts: parts}
}

// Usage holds token consumption figures from a single LLM call.
type Usage struct {
	// InputTokens is the number of tokens in the prompt sent to the model.
	InputTokens int
	// OutputTokens is the number of tokens produced by the model.
	OutputTokens int
}

// Result represents the output of an LLM execution, including the assistant message and any tool calls.
type Result struct {
	Message *Message
	// Usage holds token counts for this call. May be nil when the provider does
	// not return usage information.
	Usage *Usage
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

// ToolMiddleware wraps a tool handler.
//
// Middlewares can be used to add cross-cutting behavior (e.g. input validation,
// permission checks, logging) without baking that logic into each tool implementation.
//
// Middleware order is preserved: if you call tool.Use(m1, m2), m1 runs before m2.
type ToolMiddleware func(tool *Tool, next ToolHandler) ToolHandler

// LLMMiddleware wraps an LLM, decorating its Execute method with additional behavior.
//
// Use Use to compose multiple middlewares around a base LLM.
// Middleware order is preserved: Use(base, m1, m2) means m1 is the outermost layer and
// runs first, delegating to m2, which delegates to base.
type LLMMiddleware func(next LLM) LLM

// Use wraps base with the provided middlewares, returning a new LLM.
//
// Middlewares are applied in order: the first middleware listed is the outermost layer.
// For example, Use(base, logging, ratelimit) produces logging(ratelimit(base)).
func Use(base LLM, middlewares ...LLMMiddleware) LLM {
	result := base
	for i := len(middlewares) - 1; i >= 0; i-- {
		result = middlewares[i](result)
	}
	return result
}

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

	// middlewares wraps the tool handler to provide cross-cutting behavior
	// (e.g., input validation, permission checks, logging).
	middlewares []ToolMiddleware
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
	if name == "" {
		return nil, ErrToolNameRequired
	}

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

	schemaMap, err = ensureStrictJSONSchema(schemaMap)
	if err != nil {
		return nil, &ToolSchemaStrictnessError{Err: err}
	}

	if description != "" && schemaMap != nil {
		schemaMap["description"] = description
	}

	return &Tool{
		name:        name,
		description: description,
		inputSchema: schemaMap,
		handle:      handler,
	}, nil
}
