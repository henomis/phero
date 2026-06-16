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
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
	// ContentTypeReasoning is an extended-thinking / reasoning block returned by
	// models that expose their chain of thought (e.g. Anthropic extended thinking).
	// The Text holds the reasoning content and Signature holds the provider's
	// verification signature, which must be preserved and sent back on later turns.
	ContentTypeReasoning ContentType = "reasoning"
	// ContentTypeRedactedReasoning is an opaque, provider-encrypted reasoning block.
	// The Text holds the redacted payload verbatim; it must be sent back unchanged.
	ContentTypeRedactedReasoning ContentType = "redacted_reasoning"
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
	// Signature holds the provider verification signature for a reasoning part
	// (Type ContentTypeReasoning). It must be round-tripped unchanged so the
	// provider accepts the thinking block on subsequent turns.
	Signature string
}

// Text returns a ContentPart containing the given plain text.
func Text(s string) ContentPart {
	return ContentPart{Type: ContentTypeText, Text: s}
}

// ImageURL returns a ContentPart referencing an image at the given URL.
func ImageURL(url string) ContentPart {
	return ContentPart{Type: ContentTypeImageURL, ImageURL: url}
}

// Reasoning returns a ContentPart carrying an extended-thinking block with its
// provider verification signature.
func Reasoning(text, signature string) ContentPart {
	return ContentPart{Type: ContentTypeReasoning, Text: text, Signature: signature}
}

// RedactedReasoning returns a ContentPart carrying an opaque, provider-encrypted
// reasoning block. data is stored verbatim and must be sent back unchanged.
func RedactedReasoning(data string) ContentPart {
	return ContentPart{Type: ContentTypeRedactedReasoning, Text: data}
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
	// ToolError reports that the tool call this message answers failed.
	// Only meaningful on tool-result messages (Role == RoleTool). Providers that
	// model tool errors natively (e.g. Anthropic's tool_result is_error) use it;
	// others convey the failure through the message's text content.
	ToolError bool
	// Name is an optional participant name, used by some providers.
	Name string
}

// TextContent returns the concatenation of all text parts in the message.
func (m Message) TextContent() string {
	return TextContent(m.Parts...)
}

// ReasoningContent returns the concatenation of all reasoning (extended-thinking)
// text parts in the message. Redacted reasoning is opaque and is not included.
func (m Message) ReasoningContent() string {
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Type == ContentTypeReasoning && p.Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
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
	// CacheReadTokens is the number of input tokens served from a prompt cache.
	// Only populated by providers that report it (e.g. Anthropic prompt caching);
	// zero otherwise. These tokens are billed at the cache-read rate.
	CacheReadTokens int
	// CacheWriteTokens is the number of input tokens written to a prompt cache.
	// Only populated by providers that report it; zero otherwise. These tokens
	// are billed at the cache-write rate.
	CacheWriteTokens int
}

// Result represents the output of an LLM execution, including the assistant message and any tool calls.
type Result struct {
	Message *Message
	// Usage holds token counts for this call. May be nil when the provider does
	// not return usage information.
	Usage *Usage
	// Model is the model that produced this result, as reported by the provider
	// (which may be more specific than the requested name, e.g. a dated version).
	// Used for best-effort cost estimation; may be empty.
	Model string
}

// LLM is the minimal interface implemented by chat-model backends.
//
// Implementations are expected to accept a list of messages and return the next
// assistant message. Tools can be configured via WithTools.
type LLM interface {
	Execute(context.Context, []Message, []*Tool) (*Result, error)
}

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
