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

package tool_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/v2/llm"
	agenttool "github.com/henomis/phero/v2/tool/agent"
	"github.com/henomis/phero/v2/tool/bash"
	"github.com/henomis/phero/v2/tool/file"
	"github.com/henomis/phero/v2/tool/human"
	"github.com/henomis/phero/v2/tool/kv"
	skilltool "github.com/henomis/phero/v2/tool/skill"
	"github.com/henomis/phero/v2/tool/todo"
	"github.com/henomis/phero/v2/tool/web"
)

// jsonschemaKeys are the jsonschema tag entries the built-in tools may use.
// Anything else after a comma is the rest of a description cut by the comma.
var jsonschemaKeys = map[string]bool{
	"description": true, "enum": true, "default": true, "minimum": true,
	"maximum": true, "minLength": true, "maxLength": true, "pattern": true,
	"title": true, "format": true, "required": true, "nullable": true,
}

// TestBuiltinTools_DescriptionsAreComplete builds every built-in tool and
// checks that each schema description is the full text its tag declares.
func TestBuiltinTools_DescriptionsAreComplete(t *testing.T) {
	declared := declaredDescriptions(t)

	for _, tool := range builtinTools(t) {
		var walk func(path string, schema map[string]any)

		walk = func(path string, schema map[string]any) {
			props, _ := schema["properties"].(map[string]any)
			for name, raw := range props {
				prop, _ := raw.(map[string]any)

				desc, ok := prop["description"].(string)
				if !ok || desc == "" {
					t.Errorf("%s: property %s%s has no description", tool.Name(), path, name)
				} else if !declared[desc] {
					t.Errorf("%s: property %s%s has description %q, not one a tag declares", tool.Name(), path, name, desc)
				}

				walk(path+name+".", prop)

				if items, isMap := prop["items"].(map[string]any); isMap {
					walk(path+name+"[].", items)
				}
			}
		}

		walk("", tool.InputSchema())
	}
}

// builtinTools builds every tool the tool packages provide.
func builtinTools(t *testing.T) []*llm.Tool {
	t.Helper()

	check := func(err error) {
		t.Helper()

		if err != nil {
			t.Fatalf("build tool: %v", err)
		}
	}

	bashTool, err := bash.New()
	check(err)

	readTool, err := file.NewReadTool()
	check(err)

	writeTool, err := file.NewWriteTool()
	check(err)

	editTool, err := file.NewEditTool()
	check(err)

	multiEditTool, err := file.NewMultiEditTool()
	check(err)

	globTool, err := file.NewGlobTool()
	check(err)

	grepTool, err := file.NewGrepTool()
	check(err)

	humanTool, err := human.New(human.WithInteractor(
		func(context.Context, *human.Input) (map[string]human.Answer, error) { return nil, nil },
	))
	check(err)

	store, err := kv.New(fakeKeyValue{})
	check(err)

	kvTools, err := store.Tools()
	check(err)

	skillTool, err := skilltool.New(t.TempDir())
	check(err)

	agentTool, err := agenttool.New(llm.Func(
		func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
			return &llm.Result{}, nil
		},
	))
	check(err)

	todoTool, err := todo.New().Tool()
	check(err)

	webTool, err := web.New().Tool()
	check(err)

	return append([]*llm.Tool{
		bashTool.Tool(), bashTool.OutputTool(), bashTool.KillTool(),
		readTool.Tool(), writeTool.Tool(), editTool.Tool(), multiEditTool.Tool(),
		globTool.Tool(), grepTool.Tool(),
		humanTool.Tool(), skillTool.Tool(), agentTool.Tool(), todoTool, webTool,
	}, kvTools...)
}

// declaredDescriptions parses the tool packages' sources and returns every
// description their struct tags declare. A jsonschema tag entry that is not a
// known key fails the test: it is the tail of a description cut at a comma.
func declaredDescriptions(t *testing.T) map[string]bool {
	t.Helper()

	declared := map[string]bool{}
	fset := token.NewFileSet()

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}

		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		ast.Inspect(f, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok || field.Tag == nil {
				return true
			}

			raw, unquoteErr := strconv.Unquote(field.Tag.Value)
			if unquoteErr != nil {
				t.Errorf("%s: tag %s: %v", fset.Position(field.Pos()), field.Tag.Value, unquoteErr)
				return true
			}

			tag := reflect.StructTag(raw)

			if desc, found := tag.Lookup("jsonschema_description"); found {
				declared[desc] = true
			}

			if value, found := tag.Lookup("jsonschema"); found {
				for _, entry := range strings.Split(value, ",") {
					key, val, _ := strings.Cut(entry, "=")
					if !jsonschemaKeys[key] {
						t.Errorf("%s: jsonschema tag entry %q is not a key: a description cut at a comma, "+
							"or missing description=", fset.Position(field.Pos()), entry)
					}

					if key == "description" {
						declared[val] = true
					}
				}
			}

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("parse tool sources: %v", err)
	}

	return declared
}

// fakeKeyValue is the bucket kv.New needs to build its tools; no tool runs.
type fakeKeyValue struct {
	nats.KeyValue
}

func (fakeKeyValue) Bucket() string { return "test" }
