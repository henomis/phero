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
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/henomis/phero/v2/llm"
)

// Status is the state of a todo item.
type Status string

const (
	// Pending is an item not started yet.
	Pending Status = "pending"
	// InProgress is the item being worked on; at most one at a time.
	InProgress Status = "in_progress"
	// Completed is a finished item.
	Completed Status = "completed"
)

// Item is one task of the list.
type Item struct {
	Content string `json:"content" jsonschema:"description=What the task is, in a short imperative sentence"`
	Status  Status `json:"status" jsonschema:"enum=pending,enum=in_progress,enum=completed"`
}

// WriteInput is the input schema of the todo_write tool.
type WriteInput struct {
	Todos []Item `json:"todos" jsonschema_description:"The whole updated list, in order; it replaces the previous one"`
}

// List is an agent's task list. It is safe for concurrent use.
type List struct {
	mu       sync.Mutex
	items    []Item
	onChange func([]Item)
}

// Option configures a List.
type Option func(*List)

// WithOnChange calls fn with the new items every time the agent updates the
// list, for example to show it in a UI. fn must not keep the slice.
func WithOnChange(fn func([]Item)) Option {
	return func(l *List) { l.onChange = fn }
}

// New returns an empty List.
func New(opts ...Option) *List {
	l := &List{}
	for _, opt := range opts {
		opt(l)
	}

	return l
}

// Items returns a copy of the current items.
func (l *List) Items() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.items)
}

// Tool returns the todo_write tool that replaces the list.
func (l *List) Tool() (*llm.Tool, error) {
	return llm.NewTool("todo_write",
		"Create and update a task list for the current work. Use it for tasks with three or more steps: "+
			"write the steps, mark the one you start as in_progress (only one at a time), and mark each "+
			"completed as soon as it is done. Each call replaces the whole list.",
		l.write)
}

func (l *List) write(_ context.Context, in *WriteInput) (string, error) {
	if in == nil {
		return "", fmt.Errorf("nil input")
	}

	inProgress := 0

	for i, item := range in.Todos {
		if strings.TrimSpace(item.Content) == "" {
			return "", fmt.Errorf("item %d: %w", i+1, ErrEmptyContent)
		}

		switch item.Status {
		case InProgress:
			inProgress++
		case Pending, Completed:
		default:
			return "", fmt.Errorf("item %d: %w", i+1, ErrInvalidStatus)
		}
	}

	if inProgress > 1 {
		return "", ErrMultipleInProgress
	}

	l.mu.Lock()
	l.items = slices.Clone(in.Todos)
	items := slices.Clone(l.items)
	l.mu.Unlock()

	if l.onChange != nil {
		l.onChange(items)
	}

	return Render(items), nil
}

// Render formats items as a checklist: [x] completed, [>] in progress,
// [ ] pending, followed by a count of completed items.
func Render(items []Item) string {
	if len(items) == 0 {
		return "The task list is empty."
	}

	var b strings.Builder

	done := 0

	for _, item := range items {
		mark := "[ ]"

		switch item.Status {
		case Completed:
			mark = "[x]"
			done++
		case InProgress:
			mark = "[>]"
		case Pending:
		}

		fmt.Fprintf(&b, "%s %s\n", mark, item.Content)
	}

	fmt.Fprintf(&b, "%d of %d completed", done, len(items))

	return b.String()
}
