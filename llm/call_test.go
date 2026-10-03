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
	"errors"
	"reflect"
	"testing"
)

func newEchoTool(t *testing.T, name string) *Tool {
	t.Helper()

	tool, err := NewTool(name, "echo", func(_ context.Context, _ struct{}) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	return tool
}

func TestNewCallConfig_AppliesOptionsInOrder(t *testing.T) {
	a, b := newEchoTool(t, "a"), newEchoTool(t, "b")

	cfg := NewCallConfig(
		WithTools(a),
		nil,
		WithTools(b, nil),
		WithToolChoice(ToolChoiceRequired),
		WithForcedTool("b"),
	)

	if len(cfg.Tools) != 2 || cfg.Tools[0] != a || cfg.Tools[1] != b {
		t.Fatalf("Tools = %v, want [a b]", cfg.Tools)
	}

	want := &ToolChoice{Mode: ToolChoiceTool, Name: "b"}
	if !reflect.DeepEqual(cfg.ToolChoice, want) {
		t.Fatalf("ToolChoice = %+v, want %+v (last option wins)", cfg.ToolChoice, want)
	}
}

func TestCallConfig_Validate(t *testing.T) {
	tool := newEchoTool(t, "echo")

	format, err := NewResponseFormat[struct {
		Answer string `json:"answer"`
	}]("answer", "")
	if err != nil {
		t.Fatalf("NewResponseFormat: %v", err)
	}

	tests := []struct {
		name    string
		opts    []CallOption
		wantErr error
	}{
		{name: "empty", opts: nil},
		{name: "auto without tools", opts: []CallOption{WithToolChoice(ToolChoiceAuto)}},
		{name: "none without tools", opts: []CallOption{WithToolChoice(ToolChoiceNone)}},
		{name: "required with tools", opts: []CallOption{WithTools(tool), WithToolChoice(ToolChoiceRequired)}},
		{name: "forced offered tool", opts: []CallOption{WithTools(tool), WithForcedTool("echo")}},
		{name: "response format", opts: []CallOption{WithResponseFormat(format)}},
		{
			name:    "required without tools",
			opts:    []CallOption{WithToolChoice(ToolChoiceRequired)},
			wantErr: ErrInvalidToolChoice,
		},
		{
			name:    "forced tool not offered",
			opts:    []CallOption{WithTools(tool), WithForcedTool("other")},
			wantErr: ErrInvalidToolChoice,
		},
		{
			name:    "forced tool without name",
			opts:    []CallOption{WithTools(tool), WithForcedTool("")},
			wantErr: ErrInvalidToolChoice,
		},
		{
			name:    "tool mode via WithToolChoice has no name",
			opts:    []CallOption{WithTools(tool), WithToolChoice(ToolChoiceTool)},
			wantErr: ErrInvalidToolChoice,
		},
		{
			name:    "unknown mode",
			opts:    []CallOption{WithToolChoice("sometimes")},
			wantErr: ErrInvalidToolChoice,
		},
		{
			name:    "zero response format",
			opts:    []CallOption{WithResponseFormat(&ResponseFormat{})},
			wantErr: ErrInvalidResponseFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vErr := NewCallConfig(tt.opts...).Validate()
			if !errors.Is(vErr, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", vErr, tt.wantErr)
			}
		})
	}
}

type legacyStub struct {
	gotTools []*Tool
}

func (l *legacyStub) Execute(_ context.Context, _ []Message, tools []*Tool) (*Result, error) {
	l.gotTools = tools
	return &Result{Message: &Message{Role: RoleAssistant}}, nil
}

func TestFromLegacy_PassesTools(t *testing.T) {
	tool := newEchoTool(t, "echo")
	stub := &legacyStub{}

	if _, err := FromLegacy(stub).Execute(context.Background(), nil, WithTools(tool)); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(stub.gotTools) != 1 || stub.gotTools[0] != tool {
		t.Fatalf("legacy got tools %v, want [echo]", stub.gotTools)
	}
}

func TestFromLegacy_RejectsOptionsItCannotCarry(t *testing.T) {
	format, err := NewResponseFormat[struct {
		A string `json:"a"`
	}]("a", "")
	if err != nil {
		t.Fatalf("NewResponseFormat: %v", err)
	}

	for name, opt := range map[string]CallOption{
		"tool choice":     WithToolChoice(ToolChoiceNone),
		"response format": WithResponseFormat(format),
	} {
		t.Run(name, func(t *testing.T) {
			_, execErr := FromLegacy(&legacyStub{}).Execute(context.Background(), nil, opt)
			if !errors.Is(execErr, ErrUnsupportedCallOption) {
				t.Fatalf("Execute() = %v, want ErrUnsupportedCallOption", execErr)
			}
		})
	}
}

func TestNewResponseFormat_StrictObjectSchema(t *testing.T) {
	type CV struct {
		Name   string   `json:"name"`
		Skills []string `json:"skills"`
	}

	format, err := NewResponseFormat[*CV]("cv", "a parsed CV")
	if err != nil {
		t.Fatalf("NewResponseFormat: %v", err)
	}

	if format.Name() != "cv" || format.Description() != "a parsed CV" {
		t.Fatalf("name/description = %q/%q", format.Name(), format.Description())
	}

	schema := format.Schema()
	if schema["type"] != "object" {
		t.Fatalf("type = %v, want object", schema["type"])
	}

	if schema["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", schema["additionalProperties"])
	}

	required, _ := schema["required"].([]any)
	if len(required) != 2 {
		t.Fatalf("required = %v, want both properties", schema["required"])
	}

	for _, key := range []string{"$schema", "$id"} {
		if _, ok := schema[key]; ok {
			t.Fatalf("schema carries %s; it must be stripped", key)
		}
	}
}

func TestNewResponseFormat_Errors(t *testing.T) {
	if _, err := NewResponseFormat[struct{}](" ", ""); !errors.Is(err, ErrInvalidResponseFormat) {
		t.Fatalf("empty name: err = %v, want ErrInvalidResponseFormat", err)
	}

	var schemaErr *ResponseFormatSchemaError
	if _, err := NewResponseFormat[string]("s", ""); !errors.As(err, &schemaErr) {
		t.Fatalf("non-object root: err = %v, want *ResponseFormatSchemaError", err)
	}

	if _, err := NewResponseFormat[any]("a", ""); !errors.As(err, &schemaErr) {
		t.Fatalf("interface type: err = %v, want *ResponseFormatSchemaError", err)
	}
}

func TestNewRawResponseFormat_DoesNotMutateInput(t *testing.T) {
	in := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"answer": map[string]any{"type": "string"},
		},
	}

	format, err := NewRawResponseFormat("answer", "", in)
	if err != nil {
		t.Fatalf("NewRawResponseFormat: %v", err)
	}

	if _, ok := in["additionalProperties"]; ok {
		t.Fatal("caller's schema was mutated")
	}

	if format.Schema()["additionalProperties"] != false {
		t.Fatalf("additionalProperties = %v, want false", format.Schema()["additionalProperties"])
	}

	if _, rawErr := NewRawResponseFormat("x", "", nil); !errors.Is(rawErr, ErrInvalidResponseFormat) {
		t.Fatalf("nil schema: rawErr = %v, want ErrInvalidResponseFormat", rawErr)
	}
}

func TestNewTool_AnonymousStructInput_DoesNotPanic(t *testing.T) {
	tool, err := NewTool("greet", "", func(_ context.Context, in struct {
		Name string `json:"name"`
	},
	) (string, error) {
		return "hi " + in.Name, nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}

	props, _ := tool.InputSchema()["properties"].(map[string]any)
	if _, ok := props["name"]; !ok {
		t.Fatalf("schema = %v, want a name property", tool.InputSchema())
	}
}

func TestFunc_ImplementsLLM(t *testing.T) {
	tool := newEchoTool(t, "echo")

	var got *CallConfig

	var client LLM = Func(func(_ context.Context, _ []Message, opts ...CallOption) (*Result, error) {
		got = NewCallConfig(opts...)
		return &Result{}, nil
	})

	if _, err := client.Execute(context.Background(), nil, WithTools(tool)); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got == nil || len(got.Tools) != 1 {
		t.Fatalf("options not forwarded: %+v", got)
	}
}
