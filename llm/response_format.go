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
	"strings"
)

// ResponseFormat constrains a model's answer to a JSON schema (structured
// output). Pass it to a call with [WithResponseFormat]; the assistant message's
// text is then a JSON document matching the schema.
//
// The schema is normalized to the strict form both OpenAI and Anthropic
// require: every object closes additionalProperties and lists all its
// properties as required. The root must be an object.
type ResponseFormat struct {
	name        string
	description string
	schema      map[string]any
}

// Name returns the format's name, sent to providers that label schemas.
func (f *ResponseFormat) Name() string {
	return f.name
}

// Description returns the format's description; may be empty.
func (f *ResponseFormat) Description() string {
	return f.description
}

// Schema returns the strict JSON schema of the expected answer.
func (f *ResponseFormat) Schema() map[string]any {
	return f.schema
}

// NewResponseFormat builds a ResponseFormat whose schema is inferred from the
// Go type T, the same way [NewTool] infers a tool's input schema. T must be a
// struct or a pointer to one. Decode the answer with json.Unmarshal into a T.
//
//	type CV struct {
//		Name   string   `json:"name"`
//		Skills []string `json:"skills"`
//	}
//
//	format, err := llm.NewResponseFormat[CV]("cv", "a parsed curriculum vitae")
//	res, err := client.Execute(ctx, msgs, llm.WithResponseFormat(format))
func NewResponseFormat[T any](name, description string) (*ResponseFormat, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrInvalidResponseFormat
	}

	schema, ok := reflectSchema[T]()
	if !ok {
		return nil, &ResponseFormatSchemaError{Name: name, Err: errors.New("type parameter has nil zero value")}
	}

	schemaMap, err := mapFromJSON(schema)
	if err != nil {
		return nil, &ResponseFormatSchemaError{Name: name, Err: err}
	}

	return newResponseFormat(name, description, schemaMap)
}

// NewRawResponseFormat builds a ResponseFormat from an externally supplied JSON
// schema, for when the shape is known as data rather than as a Go type. The
// schema is deep-cloned before normalization, so the caller's map is never
// mutated.
func NewRawResponseFormat(name, description string, schema map[string]any) (*ResponseFormat, error) {
	if strings.TrimSpace(name) == "" || schema == nil {
		return nil, ErrInvalidResponseFormat
	}

	schemaMap, err := jsonEncodeDecode[map[string]any](schema)
	if err != nil {
		return nil, &ResponseFormatSchemaError{Name: name, Err: err}
	}

	return newResponseFormat(name, description, schemaMap)
}

func newResponseFormat(name, description string, schemaMap map[string]any) (*ResponseFormat, error) {
	strict, err := ensureStrictJSONSchema(schemaMap)
	if err != nil {
		return nil, &ResponseFormatSchemaError{Name: name, Err: err}
	}

	if typ, _ := strict["type"].(string); typ != schemaTypeObject {
		return nil, &ResponseFormatSchemaError{Name: name, Err: errors.New("root schema must be an object")}
	}

	// Reflection stamps the root with $schema and $id. They describe the
	// document, not the answer, and structured-output validators that accept
	// only a subset of JSON Schema keywords reject them.
	delete(strict, "$schema")
	delete(strict, "$id")

	return &ResponseFormat{
		name:        name,
		description: description,
		schema:      strict,
	}, nil
}
