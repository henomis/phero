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
	"testing"
)

type testInput struct {
	Name string `json:"name"`
}

type testOutput struct {
	NickName string `json:"nickName"`
}

func TestNewFunctionTool_PointerInput_SchemaIsStrictObject(t *testing.T) {
	tool, err := NewTool("nick", "", func(_ context.Context, in *testInput) (*testOutput, error) {
		if in == nil {
			return nil, nil
		}
		return &testOutput{NickName: in.Name + "y"}, nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if got, _ := tool.InputSchema()["type"].(string); got != "object" {
		t.Fatalf("schema type: expected %q, got %#v", "object", tool.InputSchema()["type"])
	}
	if _, ok := tool.InputSchema()["anyOf"]; ok {
		t.Fatalf("expected no top-level anyOf in schema, got: %#v", tool.InputSchema()["anyOf"])
	}

	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties: expected map, got %#v", tool.InputSchema()["properties"])
	}
	if _, ok := props["name"]; !ok {
		t.Fatalf("schema properties: expected key %q, got %#v", "name", props)
	}
}

func TestNewFunctionTool_PointerInput_HandleDecodesIntoPointer(t *testing.T) {
	var gotName string
	tool, err := NewTool("nick", "", func(_ context.Context, in *testInput) (*testOutput, error) {
		if in == nil {
			gotName = "<nil>"
			return &testOutput{NickName: "nil"}, nil
		}
		gotName = in.Name
		return &testOutput{NickName: in.Name + "y"}, nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	result, err := tool.Handle(context.Background(), `{"name":"Simone"}`)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	out, ok := result.(*testOutput)
	if !ok {
		t.Fatalf("expected *testOutput result, got %#v", result)
	}
	if gotName != "Simone" {
		t.Fatalf("handler input: expected %q, got %q", "Simone", gotName)
	}
	if out.NickName != "Simoney" {
		t.Fatalf("output: expected %q, got %q", "Simoney", out.NickName)
	}
}

func TestNewFunctionTool_PointerToAnonymousEmptyStruct_DoesNotPanic(t *testing.T) {
	type empty = struct{}

	tool, err := NewTool("noop", "", func(_ context.Context, _ *empty) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if got, _ := tool.InputSchema()["type"].(string); got != "object" {
		t.Fatalf("schema type: expected %q, got %#v", "object", tool.InputSchema()["type"])
	}
}
