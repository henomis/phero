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

package todo

import (
	"context"
	"errors"
	"testing"
)

func TestWrite_ReplacesListAndRenders(t *testing.T) {
	var changes int

	l := New(WithOnChange(func([]Item) { changes++ }))

	out, err := l.write(context.Background(), &WriteInput{Todos: []Item{
		{Content: "Read the failing test", Status: Completed},
		{Content: "Fix the parser", Status: InProgress},
		{Content: "Run the tests", Status: Pending},
	}})
	if err != nil {
		t.Fatal(err)
	}

	want := "[x] Read the failing test\n[>] Fix the parser\n[ ] Run the tests\n1 of 3 completed"
	if out != want {
		t.Fatalf("output = %q, want %q", out, want)
	}

	if _, err = l.write(context.Background(), &WriteInput{Todos: []Item{{Content: "Done", Status: Completed}}}); err != nil {
		t.Fatal(err)
	}

	if items := l.Items(); len(items) != 1 || items[0].Content != "Done" || changes != 2 {
		t.Fatalf("items = %+v, changes = %d; want the list replaced and two changes", items, changes)
	}
}

func TestWrite_Validation(t *testing.T) {
	l := New()

	cases := map[error][]Item{
		ErrEmptyContent:       {{Content: " ", Status: Pending}},
		ErrInvalidStatus:      {{Content: "x", Status: "doing"}},
		ErrMultipleInProgress: {{Content: "a", Status: InProgress}, {Content: "b", Status: InProgress}},
	}

	for want, items := range cases {
		if _, err := l.write(context.Background(), &WriteInput{Todos: items}); !errors.Is(err, want) {
			t.Errorf("items %+v: err = %v, want %v", items, err, want)
		}
	}

	if len(l.Items()) != 0 {
		t.Fatal("an invalid list must not replace the current one")
	}
}

func TestTool_SchemaHasStatusEnum(t *testing.T) {
	tool, err := New().Tool()
	if err != nil {
		t.Fatal(err)
	}

	if tool.Name() != "todo_write" {
		t.Fatalf("name = %q", tool.Name())
	}

	if _, err = tool.Handle(context.Background(), `{"todos":[{"content":"a","status":"pending"}]}`); err != nil {
		t.Fatalf("Handle: %v", err)
	}
}
