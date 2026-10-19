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

package otel

import (
	"encoding/json"

	"github.com/henomis/phero/v2/llm"
)

// Part types and modalities of the GenAI message JSON schemas.
const (
	partText         = "text"
	partToolCall     = "tool_call"
	partToolResponse = "tool_call_response"
	partReasoning    = "reasoning"
	partURI          = "uri"
	partBlob         = "blob"
	modalityImage    = "image"
)

// semMessage is a message in the gen_ai.input.messages and
// gen_ai.output.messages JSON schemas.
type semMessage struct {
	Role         string    `json:"role"`
	Parts        []semPart `json:"parts"`
	FinishReason string    `json:"finish_reason,omitempty"`
}

// semPart is a message part; which fields are set depends on Type.
type semPart struct {
	Type      string `json:"type"`
	Content   string `json:"content,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments any    `json:"arguments,omitempty"`
	Result    any    `json:"result,omitempty"`
	Modality  string `json:"modality,omitempty"`
	MIMEType  string `json:"mime_type,omitempty"`
	URI       string `json:"uri,omitempty"`
}

// semToolDef is an entry of gen_ai.tool.definitions.
type semToolDef struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// semDocument is an entry of gen_ai.retrieval.documents.
type semDocument struct {
	ID      string  `json:"id,omitempty"`
	Score   float64 `json:"score"`
	Content string  `json:"content,omitempty"`
}

func semMessageOf(m llm.Message, finish string) semMessage {
	out := semMessage{Role: m.Role, FinishReason: finish}

	if m.Role == llm.RoleTool {
		out.Parts = []semPart{{Type: partToolResponse, ID: m.ToolCallID, Result: llm.TextContent(m.Parts...)}}
		return out
	}

	out.Parts = semParts(m.Parts)

	for _, tc := range m.ToolCalls {
		out.Parts = append(out.Parts, semPart{
			Type: partToolCall, ID: tc.ID, Name: tc.Function.Name, Arguments: jsonOrString(tc.Function.Arguments),
		})
	}

	if out.Parts == nil {
		out.Parts = []semPart{}
	}

	return out
}

func semParts(parts []llm.ContentPart) []semPart {
	out := make([]semPart, 0, len(parts))

	for _, p := range parts {
		switch p.Type {
		case llm.ContentTypeText:
			out = append(out, semPart{Type: partText, Content: p.Text})
		case llm.ContentTypeReasoning:
			out = append(out, semPart{Type: partReasoning, Content: p.Text})
		case llm.ContentTypeRedactedReasoning:
			out = append(out, semPart{Type: partReasoning})
		case llm.ContentTypeImageURL:
			out = append(out, semPart{Type: partURI, Modality: modalityImage, MIMEType: p.MIMEType, URI: p.ImageURL})
		case llm.ContentTypeImageBase64:
			out = append(out, semPart{Type: partBlob, Modality: modalityImage, MIMEType: p.MIMEType, Content: p.ImageBase64})
		default:
			out = append(out, semPart{Type: string(p.Type), Content: p.Text})
		}
	}

	return out
}

// jsonOrString returns s decoded as JSON, or s itself when it is not valid JSON.
func jsonOrString(s string) any {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}

	return v
}
