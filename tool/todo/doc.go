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

// Package todo provides a Phero tool that lets an agent keep a task list
// while it works: it writes down the steps of a long task, marks the one it
// is on, and ticks them off, so it does not lose track over many turns.
//
// Each call of the todo_write tool replaces the whole list, which is simpler
// for a model than adding and removing items one by one. At most one item is
// in progress at a time.
//
//	list := todo.New(todo.WithOnChange(func(items []todo.Item) { render(items) }))
//	tool, _ := list.Tool()
//	_ = myAgent.AddTool(tool)
//
// The list lives in memory, for the life of the List: one per task or session.
package todo
