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

package mcp

import (
	"testing"
)

func TestNormalizeMCPInputSchema_NonObjectRejected(t *testing.T) {
	_, err := normalizeMCPInputSchema("not an object")
	if err == nil {
		t.Fatal("expected error for non-object input schema, got nil")
	}
}

func TestNormalizeMCPInputSchema_InsertsEmptyPropertiesWhenAbsent(t *testing.T) {
	schema := map[string]any{"type": "object"}
	result, err := normalizeMCPInputSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["properties"]; !ok {
		t.Fatal("expected 'properties' key to be inserted, but it was not")
	}
}

func TestNormalizeMCPInputSchema_PreservesExistingProperties(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	result, err := normalizeMCPInputSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := result["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties to be map[string]any, got %T", result["properties"])
	}
	if _, ok := got["name"]; !ok {
		t.Fatal("expected 'name' property to be preserved")
	}
}

func TestNormalizeMCPInputSchema_DoesNotMutateOriginalMap(t *testing.T) {
	schema := map[string]any{"type": "object"}
	_, err := normalizeMCPInputSchema(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := schema["properties"]; ok {
		t.Fatal("original schema was mutated: 'properties' key was added to the original map")
	}
}
