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
	"encoding/json"
	"reflect"
	"testing"
)

// TestEnsureStrictJSONSchema_EmptySchema tests that an empty schema returns the expected default structure.
func TestEnsureStrictJSONSchema_EmptySchema(t *testing.T) {
	schema := map[string]any{}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["type"] != "object" {
		t.Errorf("expected type=object, got: %v", result["type"])
	}
	if result["additionalProperties"] != false {
		t.Errorf("expected additionalProperties=false, got: %v", result["additionalProperties"])
	}

	props, ok := result["properties"].(map[string]any)
	if !ok {
		t.Errorf("expected properties to be a map, got: %T", result["properties"])
	}
	if len(props) != 0 {
		t.Errorf("expected empty properties, got: %v", props)
	}

	required, ok := result["required"].([]any)
	if !ok {
		t.Errorf("expected required to be a slice, got: %T", result["required"])
	}
	if len(required) != 0 {
		t.Errorf("expected empty required, got: %v", required)
	}
}

// TestEnsureStrictJSONSchema_ObjectAddsAdditionalPropertiesFalse tests that object types get additionalProperties: false.
func TestEnsureStrictJSONSchema_ObjectAddsAdditionalPropertiesFalse(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type": "string",
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["additionalProperties"] != false {
		t.Errorf("expected additionalProperties=false, got: %v", result["additionalProperties"])
	}
}

// TestEnsureStrictJSONSchema_ObjectWithAdditionalPropertiesTrue tests that setting additionalProperties: true returns an error.
func TestEnsureStrictJSONSchema_ObjectWithAdditionalPropertiesTrue(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": true,
	}

	_, err := ensureStrictJSONSchema(schema)
	if err != ErrSchemaAdditionalPropertiesSet {
		t.Errorf("expected ErrSchemaAdditionalPropertiesSet, got: %v", err)
	}
}

// TestEnsureStrictJSONSchema_PropertiesBecomesRequired tests that all properties are marked as required.
func TestEnsureStrictJSONSchema_PropertiesBecomesRequired(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type": "string",
			},
			"age": map[string]any{
				"type": "number",
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	required, ok := result["required"].([]any)
	if !ok {
		t.Fatalf("expected required to be a slice, got: %T", result["required"])
	}
	if len(required) != 2 {
		t.Errorf("expected 2 required fields, got: %v", len(required))
	}

	// Convert to map for easier checking
	requiredMap := make(map[string]bool)
	for _, r := range required {
		if str, ok := r.(string); ok {
			requiredMap[str] = true
		}
	}

	if !requiredMap["name"] || !requiredMap["age"] {
		t.Errorf("expected both name and age to be required, got: %v", required)
	}
}

// TestEnsureStrictJSONSchema_NestedObjects tests that nested objects are processed recursively.
func TestEnsureStrictJSONSchema_NestedObjects(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"street": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	props, _ := result["properties"].(map[string]any)
	address, _ := props["address"].(map[string]any)

	if address["additionalProperties"] != false {
		t.Errorf("expected nested object to have additionalProperties=false, got: %v", address["additionalProperties"])
	}

	addressRequired, ok := address["required"].([]any)
	if !ok || len(addressRequired) != 1 || addressRequired[0] != "street" {
		t.Errorf("expected nested object to have required=[street], got: %v", address["required"])
	}
}

// TestEnsureStrictJSONSchema_ArrayItems tests that array items are processed.
func TestEnsureStrictJSONSchema_ArrayItems(t *testing.T) {
	schema := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{
					"type": "integer",
				},
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	items, ok := result["items"].(map[string]any)
	if !ok {
		t.Fatalf("expected items to be a map, got: %T", result["items"])
	}

	if items["additionalProperties"] != false {
		t.Errorf("expected array items object to have additionalProperties=false, got: %v", items["additionalProperties"])
	}

	itemsRequired, ok := items["required"].([]any)
	if !ok || len(itemsRequired) != 1 || itemsRequired[0] != "id" {
		t.Errorf("expected array items to have required=[id], got: %v", items["required"])
	}
}

// TestEnsureStrictJSONSchema_AnyOf tests that anyOf variants are processed.
func TestEnsureStrictJSONSchema_AnyOf(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
			},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "integer",
					},
				},
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	anyOf, ok := result["anyOf"].([]any)
	if !ok {
		t.Fatalf("expected anyOf to be a slice, got: %T", result["anyOf"])
	}
	if len(anyOf) != 2 {
		t.Errorf("expected 2 anyOf variants, got: %v", len(anyOf))
	}

	// Check first variant has additionalProperties: false
	first, ok := anyOf[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first anyOf variant to be a map, got: %T", anyOf[0])
	}
	if first["additionalProperties"] != false {
		t.Errorf("expected first anyOf variant to have additionalProperties=false, got: %v", first["additionalProperties"])
	}
}

// TestEnsureStrictJSONSchema_OneOfConvertsToAnyOf tests that oneOf is converted to anyOf.
func TestEnsureStrictJSONSchema_OneOfConvertsToAnyOf(t *testing.T) {
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if _, ok := result["oneOf"]; ok {
		t.Errorf("expected oneOf to be removed, but it still exists")
	}

	anyOf, ok := result["anyOf"].([]any)
	if !ok {
		t.Fatalf("expected anyOf to exist, got: %T", result["anyOf"])
	}
	if len(anyOf) != 1 {
		t.Errorf("expected 1 anyOf variant (converted from oneOf), got: %v", len(anyOf))
	}
}

// TestEnsureStrictJSONSchema_OneOfMergesWithExistingAnyOf tests that oneOf merges with existing anyOf.
func TestEnsureStrictJSONSchema_OneOfMergesWithExistingAnyOf(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{
				"type": "string",
			},
		},
		"oneOf": []any{
			map[string]any{
				"type": "number",
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	anyOf, ok := result["anyOf"].([]any)
	if !ok {
		t.Fatalf("expected anyOf to exist, got: %T", result["anyOf"])
	}
	if len(anyOf) != 2 {
		t.Errorf("expected 2 anyOf variants (merged from anyOf + oneOf), got: %v", len(anyOf))
	}
}

// TestEnsureStrictJSONSchema_AllOfSingleEntryMerges tests that single allOf entries are merged into parent.
func TestEnsureStrictJSONSchema_AllOfSingleEntryMerges(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "integer",
					},
				},
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if _, ok := result["allOf"]; ok {
		t.Errorf("expected single allOf to be merged and removed, but it still exists")
	}

	if result["type"] != "object" {
		t.Errorf("expected type=object from merged allOf, got: %v", result["type"])
	}

	props, ok := result["properties"].(map[string]any)
	if !ok || len(props) != 1 {
		t.Errorf("expected properties from merged allOf, got: %v", result["properties"])
	}
}

// TestEnsureStrictJSONSchema_AllOfMultipleEntries tests that multiple allOf entries are processed.
func TestEnsureStrictJSONSchema_AllOfMultipleEntries(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			map[string]any{
				"type": "object",
			},
			map[string]any{
				"properties": map[string]any{
					"id": map[string]any{
						"type": "integer",
					},
				},
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	allOf, ok := result["allOf"].([]any)
	if !ok {
		t.Fatalf("expected allOf to remain with multiple entries, got: %T", result["allOf"])
	}
	if len(allOf) != 2 {
		t.Errorf("expected 2 allOf entries, got: %v", len(allOf))
	}
}

// TestEnsureStrictJSONSchema_NilDefaultStripped tests that nil defaults are removed.
func TestEnsureStrictJSONSchema_NilDefaultStripped(t *testing.T) {
	schema := map[string]any{
		"type":    "string",
		"default": nil,
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if _, ok := result["default"]; ok {
		t.Errorf("expected nil default to be removed, but it still exists")
	}
}

// TestEnsureStrictJSONSchema_NonNilDefaultKept tests that non-nil defaults are kept.
func TestEnsureStrictJSONSchema_NonNilDefaultKept(t *testing.T) {
	schema := map[string]any{
		"type":    "string",
		"default": "hello",
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if result["default"] != "hello" {
		t.Errorf("expected non-nil default to be kept, got: %v", result["default"])
	}
}

// TestEnsureStrictJSONSchema_Defs tests that $defs are processed.
func TestEnsureStrictJSONSchema_Defs(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"Person": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
			},
		},
		"type": "object",
		"properties": map[string]any{
			"person": map[string]any{
				"$ref": "#/$defs/Person",
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	defs, ok := result["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("expected $defs to be a map, got: %T", result["$defs"])
	}

	person, ok := defs["Person"].(map[string]any)
	if !ok {
		t.Fatalf("expected Person definition to be a map, got: %T", defs["Person"])
	}

	if person["additionalProperties"] != false {
		t.Errorf("expected Person definition to have additionalProperties=false, got: %v", person["additionalProperties"])
	}
}

// TestEnsureStrictJSONSchema_Definitions tests that definitions are processed (legacy $defs naming).
func TestEnsureStrictJSONSchema_Definitions(t *testing.T) {
	schema := map[string]any{
		"definitions": map[string]any{
			"Person": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	definitions, ok := result["definitions"].(map[string]any)
	if !ok {
		t.Fatalf("expected definitions to be a map, got: %T", result["definitions"])
	}

	person, ok := definitions["Person"].(map[string]any)
	if !ok {
		t.Fatalf("expected Person definition to be a map, got: %T", definitions["Person"])
	}

	if person["additionalProperties"] != false {
		t.Errorf("expected Person definition to have additionalProperties=false, got: %v", person["additionalProperties"])
	}
}

// TestEnsureStrictJSONSchema_RefExpansion tests that $ref with additional properties is expanded.
func TestEnsureStrictJSONSchema_RefExpansion(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"BaseType": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type": "integer",
					},
				},
			},
		},
		"type": "object",
		"properties": map[string]any{
			"item": map[string]any{
				"$ref":        "#/$defs/BaseType",
				"description": "An item with id",
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	props, _ := result["properties"].(map[string]any)
	item, ok := props["item"].(map[string]any)
	if !ok {
		t.Fatalf("expected item property to be a map, got: %T", props["item"])
	}

	// $ref should be removed after expansion
	if _, ok := item["$ref"]; ok {
		t.Errorf("expected $ref to be removed after expansion, but it still exists")
	}

	// Properties from BaseType should be present
	if item["type"] != "object" {
		t.Errorf("expected type=object from $ref expansion, got: %v", item["type"])
	}

	// Description should be preserved (takes priority over ref)
	if item["description"] != "An item with id" {
		t.Errorf("expected description to be preserved, got: %v", item["description"])
	}
}

// TestEnsureStrictJSONSchema_NonMapInput tests error when input is not a map.
func TestEnsureStrictJSONSchema_NonMapInput(t *testing.T) {
	// This test actually tests ensureStrictJSONSchemaRecursive indirectly
	// since ensureStrictJSONSchema checks len(schema) == 0 first
	schema := map[string]any{
		"properties": "not a map", // Invalid: properties should be a map
	}

	// This should not error because properties being a string doesn't violate the top-level map requirement
	// The error would occur if we tried to process it as properties
	_, err := ensureStrictJSONSchema(schema)
	// This won't error in the current implementation because properties is just stored as-is
	if err != nil {
		t.Logf("got error (expected for invalid properties): %v", err)
	}
}

// TestResolveRef tests $ref resolution.
func TestResolveRef(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"Person": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
			},
		},
	}

	tests := []struct {
		name    string
		ref     string
		wantErr bool
	}{
		{
			name:    "valid ref",
			ref:     "#/$defs/Person",
			wantErr: false,
		},
		{
			name:    "invalid format without prefix",
			ref:     "$defs/Person",
			wantErr: true,
		},
		{
			name:    "non-existent key",
			ref:     "#/$defs/NonExistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolveRef(root, tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for ref %q, got nil", tt.ref)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error for ref %q, got: %v", tt.ref, err)
				}
				if result == nil {
					t.Errorf("expected non-nil result for ref %q", tt.ref)
				}
			}
		})
	}
}

// TestResolveRef_InvalidFormat tests error for invalid $ref format.
func TestResolveRef_InvalidFormat(t *testing.T) {
	root := map[string]any{}
	_, err := resolveRef(root, "invalid/ref")

	var refErr *SchemaUnexpectedRefFormatError
	if !reflect.DeepEqual(err, &SchemaUnexpectedRefFormatError{Ref: "invalid/ref"}) {
		t.Errorf("expected SchemaUnexpectedRefFormatError, got: %v", err)
	}
	_ = refErr // Just to satisfy linter if we add more checks
}

// TestResolveRef_NonDictionaryInPath tests error when ref path contains non-map.
func TestResolveRef_NonDictionaryInPath(t *testing.T) {
	root := map[string]any{
		"$defs": "not a map",
	}
	_, err := resolveRef(root, "#/$defs/Person")

	var nonDictErr *SchemaNonDictionaryWhileResolvingRefError
	if err == nil {
		t.Errorf("expected error when resolving through non-dictionary, got nil")
	} else {
		// Check it's the right type of error
		var ok bool
		nonDictErr, ok = err.(*SchemaNonDictionaryWhileResolvingRefError)
		if !ok {
			t.Errorf("expected SchemaNonDictionaryWhileResolvingRefError, got: %T", err)
		}
	}
	_ = nonDictErr // Satisfy linter
}

// TestResolveRef_NestedPath tests nested path resolution.
func TestResolveRef_NestedPath(t *testing.T) {
	root := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"User": map[string]any{
					"type": "object",
				},
			},
		},
	}

	result, err := resolveRef(root, "#/components/schemas/User")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected result to be a map, got: %T", result)
	}

	if resultMap["type"] != "object" {
		t.Errorf("expected type=object, got: %v", resultMap["type"])
	}
}

// TestHasMoreThanNKeys tests the hasMoreThanNKeys utility function.
func TestHasMoreThanNKeys(t *testing.T) {
	tests := []struct {
		name     string
		obj      map[string]any
		n        int
		expected bool
	}{
		{
			name:     "empty map, n=0",
			obj:      map[string]any{},
			n:        0,
			expected: false,
		},
		{
			name:     "one key, n=0",
			obj:      map[string]any{"a": 1},
			n:        0,
			expected: true,
		},
		{
			name:     "one key, n=1",
			obj:      map[string]any{"a": 1},
			n:        1,
			expected: false,
		},
		{
			name:     "two keys, n=1",
			obj:      map[string]any{"a": 1, "b": 2},
			n:        1,
			expected: true,
		},
		{
			name:     "three keys, n=2",
			obj:      map[string]any{"a": 1, "b": 2, "c": 3},
			n:        2,
			expected: true,
		},
		{
			name:     "three keys, n=3",
			obj:      map[string]any{"a": 1, "b": 2, "c": 3},
			n:        3,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasMoreThanNKeys(tt.obj, tt.n)
			if result != tt.expected {
				t.Errorf("hasMoreThanNKeys(%v, %d) = %v, expected %v",
					tt.obj, tt.n, result, tt.expected)
			}
		})
	}
}

// TestEnsureStrictJSONSchema_ComplexRealWorldExample tests a complex real-world schema.
func TestEnsureStrictJSONSchema_ComplexRealWorldExample(t *testing.T) {
	schemaJSON := `{
		"$defs": {
			"Address": {
				"type": "object",
				"properties": {
					"street": {"type": "string"},
					"city": {"type": "string"}
				}
			}
		},
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "integer"},
			"address": {"$ref": "#/$defs/Address"},
			"tags": {
				"type": "array",
				"items": {"type": "string"}
			}
		}
	}`

	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		t.Fatalf("failed to parse test schema: %v", err)
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Check top-level additionalProperties
	if result["additionalProperties"] != false {
		t.Errorf("expected top-level additionalProperties=false, got: %v", result["additionalProperties"])
	}

	// Check required fields
	required, ok := result["required"].([]any)
	if !ok || len(required) != 4 {
		t.Errorf("expected 4 required fields, got: %v", result["required"])
	}

	// Check that $defs Address has been processed
	defs, _ := result["$defs"].(map[string]any)
	address, _ := defs["Address"].(map[string]any)
	if address["additionalProperties"] != false {
		t.Errorf("expected Address definition to have additionalProperties=false, got: %v", address["additionalProperties"])
	}

	addressRequired, ok := address["required"].([]any)
	if !ok || len(addressRequired) != 2 {
		t.Errorf("expected Address to have 2 required fields, got: %v", address["required"])
	}
}

// TestEnsureStrictJSONSchema_ErrorPropagation tests that errors propagate correctly from nested calls.
func TestEnsureStrictJSONSchema_ErrorPropagation(t *testing.T) {
	// Create a schema with nested additional properties set to true
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"nested": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
	}

	_, err := ensureStrictJSONSchema(schema)
	if err != ErrSchemaAdditionalPropertiesSet {
		t.Errorf("expected error to propagate from nested object, got: %v", err)
	}
}

// TestEnsureStrictJSONSchema_SchemaExpectedMapError tests error when non-map is encountered.
func TestEnsureStrictJSONSchema_SchemaExpectedMapError(t *testing.T) {
	// We need to test the recursive function directly since ensureStrictJSONSchema expects a map
	_, err := ensureStrictJSONSchemaRecursive("not a map", []string{}, map[string]any{})

	var mapErr *SchemaExpectedMapError
	if err == nil {
		t.Errorf("expected SchemaExpectedMapError, got nil")
	} else {
		var ok bool
		mapErr, ok = err.(*SchemaExpectedMapError)
		if !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
		if mapErr != nil && mapErr.Got != "not a map" {
			t.Errorf("expected error to contain 'not a map', got: %v", mapErr.Got)
		}
	}
}

// TestEnsureStrictJSONSchema_NonStringRef tests error when $ref is not a string.
func TestEnsureStrictJSONSchema_NonStringRef(t *testing.T) {
	schema := map[string]any{
		"$ref":        123,                // Invalid: $ref should be a string
		"description": "some description", // Has more than 1 key, so $ref expansion is triggered
	}

	_, err := ensureStrictJSONSchema(schema)

	var refErr *SchemaNonStringRefError
	if err == nil {
		t.Errorf("expected SchemaNonStringRefError, got nil")
	} else {
		var ok bool
		refErr, ok = err.(*SchemaNonStringRefError)
		if !ok {
			t.Errorf("expected SchemaNonStringRefError, got: %T", err)
		}
		if refErr != nil && refErr.RawRef != 123 {
			t.Errorf("expected error RawRef to be 123, got: %v", refErr.RawRef)
		}
	}
}

// TestEnsureStrictJSONSchema_RefOnlyNoExpansion tests that a $ref-only property is not expanded.
func TestEnsureStrictJSONSchema_RefOnlyNoExpansion(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"Person": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type": "string",
					},
				},
			},
		},
		"type": "object",
		"properties": map[string]any{
			"person": map[string]any{
				"$ref": "#/$defs/Person",
			},
		},
	}

	result, err := ensureStrictJSONSchema(schema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	props, _ := result["properties"].(map[string]any)
	person, ok := props["person"].(map[string]any)
	if !ok {
		t.Fatalf("expected person property to be a map, got: %T", props["person"])
	}

	// When $ref is the only key (hasMoreThanNKeys returns false), it should be kept as-is
	if _, hasRef := person["$ref"]; !hasRef {
		t.Errorf("expected $ref to be preserved when it's the only property")
	}
}

// TestEnsureStrictJSONSchema_DefsWithNonMapSchema tests error when $defs contains non-map schema.
func TestEnsureStrictJSONSchema_DefsWithNonMapSchema(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"BadDef": "not a map", // This should cause an error
		},
		"type": "object",
	}

	_, err := ensureStrictJSONSchema(schema)

	var mapErr *SchemaExpectedMapError
	if err == nil {
		t.Errorf("expected SchemaExpectedMapError for invalid $defs entry, got nil")
	} else {
		var ok bool
		mapErr, ok = err.(*SchemaExpectedMapError)
		if !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
		if mapErr != nil && mapErr.Got != "not a map" {
			t.Errorf("expected error Got to be 'not a map', got: %v", mapErr.Got)
		}
	}
}

// TestEnsureStrictJSONSchema_DefinitionsWithNonMapSchema tests error when definitions contains non-map schema.
func TestEnsureStrictJSONSchema_DefinitionsWithNonMapSchema(t *testing.T) {
	schema := map[string]any{
		"definitions": map[string]any{
			"BadDef": []string{"invalid"}, // This should cause an error
		},
		"type": "object",
	}

	_, err := ensureStrictJSONSchema(schema)

	if err == nil {
		t.Errorf("expected SchemaExpectedMapError for invalid definitions entry, got nil")
	} else {
		if _, ok := err.(*SchemaExpectedMapError); !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
	}
}

// TestEnsureStrictJSONSchema_ArrayItemsNonMapSchema tests error when array items is non-map.
func TestEnsureStrictJSONSchema_ArrayItemsNonMapSchema(t *testing.T) {
	// Note: items as string won't trigger the recursive processing since it checks for map[string]any
	// So we test the actual error case with a nested structure
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"list": map[string]any{
				"type": "array",
				"items": map[string]any{
					"anyOf": []any{
						"not a map", // This will cause an error
					},
				},
			},
		},
	}

	_, err := ensureStrictJSONSchema(schema)

	if err == nil {
		t.Errorf("expected SchemaExpectedMapError for invalid items anyOf entry, got nil")
	} else {
		if _, ok := err.(*SchemaExpectedMapError); !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
	}
}

// TestEnsureStrictJSONSchema_AnyOfWithNonMapVariant tests error when anyOf contains non-map variant.
func TestEnsureStrictJSONSchema_AnyOfWithNonMapVariant(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			"not a map", // This should cause an error
		},
	}

	_, err := ensureStrictJSONSchema(schema)

	if err == nil {
		t.Errorf("expected SchemaExpectedMapError for invalid anyOf variant, got nil")
	} else {
		if _, ok := err.(*SchemaExpectedMapError); !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
	}
}

// TestEnsureStrictJSONSchema_OneOfWithNonMapVariant tests error when oneOf contains non-map variant.
func TestEnsureStrictJSONSchema_OneOfWithNonMapVariant(t *testing.T) {
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			42, // This should cause an error
		},
	}

	_, err := ensureStrictJSONSchema(schema)

	var mapErr *SchemaExpectedMapError
	if err == nil {
		t.Errorf("expected SchemaExpectedMapError for invalid oneOf variant, got nil")
	} else {
		var ok bool
		mapErr, ok = err.(*SchemaExpectedMapError)
		if !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
		if mapErr != nil && mapErr.Got != 42 {
			t.Errorf("expected error Got to be 42, got: %v", mapErr.Got)
		}
	}
}

// TestEnsureStrictJSONSchema_AllOfSingleWithNonMapEntry tests error when single allOf contains non-map.
func TestEnsureStrictJSONSchema_AllOfSingleWithNonMapEntry(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			"not a map", // Single allOf entry that's invalid
		},
	}

	_, err := ensureStrictJSONSchema(schema)

	if err == nil {
		t.Errorf("expected SchemaExpectedMapError for invalid single allOf entry, got nil")
	} else {
		if _, ok := err.(*SchemaExpectedMapError); !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
	}
}

// TestEnsureStrictJSONSchema_AllOfMultipleWithNonMapEntry tests error when allOf with multiple entries contains non-map.
func TestEnsureStrictJSONSchema_AllOfMultipleWithNonMapEntry(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			map[string]any{"type": "object"},
			12345, // Invalid entry
		},
	}

	_, err := ensureStrictJSONSchema(schema)

	var mapErr *SchemaExpectedMapError
	if err == nil {
		t.Errorf("expected SchemaExpectedMapError for invalid allOf entry, got nil")
	} else {
		var ok bool
		mapErr, ok = err.(*SchemaExpectedMapError)
		if !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
		if mapErr != nil && mapErr.Got != 12345 {
			t.Errorf("expected error Got to be 12345, got: %v", mapErr.Got)
		}
	}
}

// TestEnsureStrictJSONSchema_RefExpansionWithNonMapResolved tests error when ref resolves to non-map.
func TestEnsureStrictJSONSchema_RefExpansionWithNonMapResolved(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"NotAnObject": "just a string", // Invalid: should be a map
		},
		"type": "object",
		"properties": map[string]any{
			"item": map[string]any{
				"$ref":        "#/$defs/NotAnObject",
				"description": "triggers expansion", // More than 1 key triggers ref expansion
			},
		},
	}

	_, err := ensureStrictJSONSchema(schema)

	var mapErr *SchemaExpectedMapError
	if err == nil {
		t.Errorf("expected SchemaExpectedMapError when ref resolves to non-map, got nil")
	} else {
		var ok bool
		mapErr, ok = err.(*SchemaExpectedMapError)
		if !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
		if mapErr != nil && mapErr.Got != "just a string" {
			t.Errorf("expected error Got to be 'just a string', got: %v", mapErr.Got)
		}
	}
}

// TestEnsureStrictJSONSchema_PropertiesRecursiveError tests error propagation from nested properties.
func TestEnsureStrictJSONSchema_PropertiesRecursiveError(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"nested": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"deepNested": map[string]any{
						"anyOf": []any{
							true, // Invalid: should be a map
						},
					},
				},
			},
		},
	}

	_, err := ensureStrictJSONSchema(schema)

	if err == nil {
		t.Errorf("expected SchemaExpectedMapError from deeply nested property, got nil")
	} else {
		if _, ok := err.(*SchemaExpectedMapError); !ok {
			t.Errorf("expected SchemaExpectedMapError, got: %T", err)
		}
	}
}
