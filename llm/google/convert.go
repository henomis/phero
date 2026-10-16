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
	"encoding/base64"
	"encoding/json"
	"maps"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/genai"

	"github.com/henomis/phero/v2/llm"
)

// Signature tags. A thought signature is kept in a reasoning part whose
// Signature reads "genai:<kind>:<base64 signature>[:<tool call id>]".
const (
	sigPrefix  = "genai:"
	sigCall    = "call"
	sigText    = "text"
	sigThought = "thought"
	// sigFields is kind, signature and id; sigMinFields is kind and signature.
	sigFields    = 3
	sigMinFields = 2
)

// generatedIDPrefix marks a tool call ID this package made up because Gemini
// sent none; it is not sent back to Gemini.
const generatedIDPrefix = "phero-"

// Gemini function response keys, as Google's documentation suggests.
const (
	responseOutput = "output"
	responseError  = "error"
)

// signatures are the thought signatures of one assistant message, by the part
// they belong to.
type signatures struct {
	calls   map[string][]byte
	text    []byte
	thought []byte
}

// encodeSignature returns the reasoning part that carries sig for a part of
// kind (sigCall with the tool call id, sigText or sigThought).
func encodeSignature(kind string, sig []byte, id string) llm.ContentPart {
	s := sigPrefix + kind + ":" + base64.StdEncoding.EncodeToString(sig)
	if id != "" {
		s += ":" + id
	}

	return llm.Reasoning("", s)
}

// signaturesFromParts collects the signatures this package stored in an
// assistant message's reasoning parts, ignoring any other reasoning.
func signaturesFromParts(parts []llm.ContentPart) signatures {
	sigs := signatures{calls: map[string][]byte{}}

	for _, p := range parts {
		if p.Type != llm.ContentTypeReasoning || !strings.HasPrefix(p.Signature, sigPrefix) {
			continue
		}

		// kind:base64[:id]; the id comes last, so it may itself hold colons.
		fields := strings.SplitN(strings.TrimPrefix(p.Signature, sigPrefix), ":", sigFields)
		if len(fields) < sigMinFields {
			continue
		}

		sig, err := base64.StdEncoding.DecodeString(fields[1])
		if err != nil || len(sig) == 0 {
			continue
		}

		switch fields[0] {
		case sigCall:
			if len(fields) == sigFields {
				sigs.calls[fields[2]] = sig
			}
		case sigText:
			sigs.text = sig
		case sigThought:
			sigs.thought = sig
		}
	}

	return sigs
}

// messageFromParts converts the parts of a Gemini answer to a phero assistant
// message. Text parts are joined, thoughts become one reasoning part, function
// calls become tool calls, and every thought signature is kept in a
// signature-only reasoning part (see the package documentation).
func messageFromParts(parts []*genai.Part) *llm.Message {
	var (
		text, thought strings.Builder
		sigParts      []llm.ContentPart
		toolCalls     []llm.ToolCall
		textSig       []byte
		thoughtSig    []byte
	)

	for _, p := range parts {
		if p == nil {
			continue
		}

		switch {
		case p.FunctionCall != nil:
			tc := toolCallFromGenai(p.FunctionCall)
			toolCalls = append(toolCalls, tc)

			if len(p.ThoughtSignature) > 0 {
				sigParts = append(sigParts, encodeSignature(sigCall, p.ThoughtSignature, tc.ID))
			}
		case p.Thought:
			thought.WriteString(p.Text)

			if len(p.ThoughtSignature) > 0 {
				thoughtSig = p.ThoughtSignature
			}
		default:
			text.WriteString(p.Text)

			if len(p.ThoughtSignature) > 0 {
				textSig = p.ThoughtSignature
			}
		}
	}

	out := make([]llm.ContentPart, 0, len(sigParts)+3) //nolint:mnd // thought, its signature, text
	if thought.Len() > 0 {
		out = append(out, llm.Reasoning(thought.String(), ""))
	}

	if thoughtSig != nil {
		out = append(out, encodeSignature(sigThought, thoughtSig, ""))
	}

	out = append(out, sigParts...)

	if textSig != nil {
		out = append(out, encodeSignature(sigText, textSig, ""))
	}

	if text.Len() > 0 {
		out = append(out, llm.Text(text.String()))
	}

	return &llm.Message{Role: llm.RoleAssistant, Parts: out, ToolCalls: toolCalls}
}

// toolCallFromGenai converts a Gemini function call; one without an id gets a
// generated one, so its result can still be matched to it.
func toolCallFromGenai(fc *genai.FunctionCall) llm.ToolCall {
	id := fc.ID
	if id == "" {
		id = generatedIDPrefix + uuid.NewString()
	}

	args := "{}"

	if len(fc.Args) > 0 {
		if b, err := json.Marshal(fc.Args); err == nil {
			args = string(b)
		}
	}

	return llm.ToolCall{
		ID:       id,
		Type:     llm.ToolTypeFunction,
		Function: llm.FunctionCall{Name: fc.Name, Arguments: args},
	}
}

// contentsFromMessages converts phero messages to Gemini contents. System
// messages are joined into the system instruction. Consecutive messages of the
// same Gemini role are merged into one content: in particular the results of
// parallel tool calls must reach Gemini as one turn.
//
//nolint:gocognit,cyclop,funlen
func contentsFromMessages(messages []llm.Message) (*genai.Content, []*genai.Content, error) {
	var (
		systemParts []string
		out         []*genai.Content
	)

	// callNames maps tool call ids to function names: a function response
	// must name the function it answers.
	callNames := map[string]string{}

	add := func(role string, parts ...*genai.Part) {
		if len(parts) == 0 {
			return
		}

		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Parts = append(out[n-1].Parts, parts...)
			return
		}

		out = append(out, &genai.Content{Role: role, Parts: parts})
	}

	for _, m := range messages {
		switch m.Role {
		case llm.RoleSystem:
			if text := m.TextContent(); strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}

		case llm.RoleUser:
			parts, err := partsFromContent(m.Parts)
			if err != nil {
				return nil, nil, err
			}

			add(genai.RoleUser, parts...)

		case llm.RoleAssistant:
			parts, err := assistantParts(m, callNames)
			if err != nil {
				return nil, nil, err
			}

			add(genai.RoleModel, parts...)

		case llm.RoleTool:
			id := strings.TrimSpace(m.ToolCallID)
			if id == "" {
				return nil, nil, ErrToolMessageMissingToolCallID
			}

			name, ok := callNames[id]
			if !ok {
				return nil, nil, &UnknownToolCallError{ToolCallID: id}
			}

			resp, err := functionResponse(m, id, name)
			if err != nil {
				return nil, nil, err
			}

			add(genai.RoleUser, &genai.Part{FunctionResponse: resp})

		default:
			return nil, nil, &UnsupportedRoleError{Role: m.Role}
		}
	}

	var system *genai.Content
	if joined := strings.TrimSpace(strings.Join(systemParts, "\n\n")); joined != "" {
		system = &genai.Content{Parts: []*genai.Part{{Text: joined}}}
	}

	return system, out, nil
}

// assistantParts rebuilds the model turn of an assistant message, putting
// each stored thought signature back on the part Gemini attached it to, and
// records the message's tool calls in callNames.
func assistantParts(m llm.Message, callNames map[string]string) ([]*genai.Part, error) {
	sigs := signaturesFromParts(m.Parts)

	var parts []*genai.Part

	// A thought goes back only with its signature: a bare summary carries
	// nothing the model needs.
	if sigs.thought != nil {
		parts = append(parts, &genai.Part{Text: m.ReasoningContent(), Thought: true, ThoughtSignature: sigs.thought})
	}

	if text := m.TextContent(); text != "" || sigs.text != nil {
		parts = append(parts, &genai.Part{Text: text, ThoughtSignature: sigs.text})
	}

	for _, tc := range m.ToolCalls {
		args := map[string]any{}
		if raw := strings.TrimSpace(tc.Function.Arguments); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				return nil, &ToolArgumentsParseError{ToolName: tc.Function.Name, Err: err}
			}
		}

		callNames[tc.ID] = tc.Function.Name

		fc := &genai.FunctionCall{Name: tc.Function.Name, Args: args}
		if !strings.HasPrefix(tc.ID, generatedIDPrefix) {
			fc.ID = tc.ID
		}

		parts = append(parts, &genai.Part{FunctionCall: fc, ThoughtSignature: sigs.calls[tc.ID]})
	}

	return parts, nil
}

// functionResponse converts a tool result: its text becomes the response's
// "output" (or "error" when the tool failed) and its images ride along as
// inline data.
func functionResponse(m llm.Message, id, name string) (*genai.FunctionResponse, error) {
	key := responseOutput
	if m.ToolError {
		key = responseError
	}

	resp := &genai.FunctionResponse{Name: name, Response: map[string]any{key: m.TextContent()}}
	if !strings.HasPrefix(id, generatedIDPrefix) {
		resp.ID = id
	}

	for _, p := range m.Parts {
		switch p.Type {
		case llm.ContentTypeImageBase64:
			data, err := base64.StdEncoding.DecodeString(p.ImageBase64)
			if err != nil {
				return nil, &InvalidImageError{Err: err}
			}

			resp.Parts = append(resp.Parts, &genai.FunctionResponsePart{
				InlineData: &genai.FunctionResponseBlob{MIMEType: p.MIMEType, Data: data},
			})
		case llm.ContentTypeImageURL:
			return nil, ErrImageURLUnsupported
		case llm.ContentTypeText, llm.ContentTypeReasoning, llm.ContentTypeRedactedReasoning:
			// text is the response; reasoning has no place in a tool result
		}
	}

	return resp, nil
}

// partsFromContent converts the content of a user message.
func partsFromContent(content []llm.ContentPart) ([]*genai.Part, error) {
	parts := make([]*genai.Part, 0, len(content))

	for _, p := range content {
		switch p.Type {
		case llm.ContentTypeText:
			if strings.TrimSpace(p.Text) != "" {
				parts = append(parts, &genai.Part{Text: p.Text})
			}
		case llm.ContentTypeImageBase64:
			data, err := base64.StdEncoding.DecodeString(p.ImageBase64)
			if err != nil {
				return nil, &InvalidImageError{Err: err}
			}

			parts = append(parts, &genai.Part{InlineData: &genai.Blob{MIMEType: p.MIMEType, Data: data}})
		case llm.ContentTypeImageURL:
			return nil, ErrImageURLUnsupported
		case llm.ContentTypeReasoning, llm.ContentTypeRedactedReasoning:
			// reasoning belongs to model turns
		}
	}

	return parts, nil
}

// toolsToGenai declares the tools as functions, with their JSON schemas as
// they are.
func toolsToGenai(tools []*llm.Tool) []*genai.Tool {
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))

	for _, t := range tools {
		if t == nil {
			continue
		}

		schema := t.InputSchema()
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}

		decls = append(decls, &genai.FunctionDeclaration{
			Name:                 t.Name(),
			Description:          t.Description(),
			ParametersJsonSchema: schema,
		})
	}

	return []*genai.Tool{{FunctionDeclarations: decls}}
}

// toolChoiceToGenai maps a tool choice to Gemini's function calling config:
// "required" is ANY, and a forced tool is ANY restricted to that function.
func toolChoiceToGenai(tc *llm.ToolChoice) *genai.FunctionCallingConfig {
	switch tc.Mode {
	case llm.ToolChoiceNone:
		return &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeNone}
	case llm.ToolChoiceRequired:
		return &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAny}
	case llm.ToolChoiceTool:
		return &genai.FunctionCallingConfig{
			Mode:                 genai.FunctionCallingConfigModeAny,
			AllowedFunctionNames: []string{tc.Name},
		}
	case llm.ToolChoiceAuto:
	}

	return &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto}
}

// outputSchema returns the response schema. Gemini has no separate
// description field, so a format's description becomes the schema's own
// top-level description, on a copy.
func outputSchema(f *llm.ResponseFormat) map[string]any {
	schema := f.Schema()
	if f.Description() == "" {
		return schema
	}

	if _, ok := schema["description"]; ok {
		return schema
	}

	out := make(map[string]any, len(schema)+1)
	maps.Copy(out, schema)
	out["description"] = f.Description()

	return out
}
