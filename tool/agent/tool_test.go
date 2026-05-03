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

package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/henomis/phero/llm"
)

type stubLLM struct {
	execute func(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error)
}

func (s stubLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	return s.execute(ctx, messages, tools)
}

func TestNewRequiresLLM(t *testing.T) {
	_, err := New(nil)
	if !errors.Is(err, ErrLLMRequired) {
		t.Fatalf("New() error = %v, want %v", err, ErrLLMRequired)
	}
}

func TestNewExposesFixedToolIdentity(t *testing.T) {
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return &llm.Result{Message: &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("ok")}}}, nil
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := tool.Tool().Name(); got != toolName {
		t.Fatalf("Tool().Name() = %q, want %q", got, toolName)
	}
	if got := tool.Tool().Description(); got != toolDescription {
		t.Fatalf("Tool().Description() = %q, want %q", got, toolDescription)
	}
}

func TestHandleNilInput(t *testing.T) {
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return nil, errors.New("should not be called")
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `null`)
	if !errors.Is(err, ErrNilInput) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrNilInput)
	}
}

func TestHandleNameRequired(t *testing.T) {
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return nil, errors.New("should not be called")
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `{"name":"","description":"a desc","input":"do it"}`)
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrNameRequired)
	}
}

func TestHandleDescriptionRequired(t *testing.T) {
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return nil, errors.New("should not be called")
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `{"name":"agent","description":"","input":"do it"}`)
	if !errors.Is(err, ErrDescriptionRequired) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrDescriptionRequired)
	}
}

func TestHandleInputRequired(t *testing.T) {
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return nil, errors.New("should not be called")
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `{"name":"agent","description":"a desc","input":"   "}`)
	if !errors.Is(err, ErrInputRequired) {
		t.Fatalf("Handle() error = %v, want %v", err, ErrInputRequired)
	}
}

func TestHandleSuccess(t *testing.T) {
	var gotMessages []llm.Message
	var gotTools []*llm.Tool

	extraTool, err := llm.NewTool("echo", "echo tool", func(_ context.Context, _ *struct{}) (string, error) {
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("llm.NewTool() error = %v", err)
	}

	tool, err := New(stubLLM{execute: func(_ context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
		gotMessages = messages
		gotTools = tools
		return &llm.Result{Message: &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("done")}}}, nil
	}}, extraTool)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	res, err := tool.Tool().Handle(context.Background(), `{"name":"researcher","description":"You are a researcher","input":"solve this"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	out, ok := res.(*Output)
	if !ok {
		t.Fatalf("Handle() result type = %T, want *Output", res)
	}
	if out.Output != "done" {
		t.Fatalf("output = %q, want %q", out.Output, "done")
	}

	if len(gotMessages) != 2 {
		t.Fatalf("messages length = %d, want %d", len(gotMessages), 2)
	}
	if got := gotMessages[0].Role; got != llm.RoleSystem {
		t.Fatalf("system role = %q, want %q", got, llm.RoleSystem)
	}
	if got := gotMessages[0].TextContent(); got != "You are a researcher" {
		t.Fatalf("system description = %q, want %q", got, "You are a researcher")
	}
	if got := gotMessages[1].Role; got != llm.RoleUser {
		t.Fatalf("user role = %q, want %q", got, llm.RoleUser)
	}
	if got := gotMessages[1].TextContent(); got != "solve this" {
		t.Fatalf("user input = %q, want %q", got, "solve this")
	}

	if len(gotTools) != 1 {
		t.Fatalf("tools length = %d, want %d", len(gotTools), 1)
	}
	if gotTools[0].Name() != "echo" {
		t.Fatalf("tool name = %q, want %q", gotTools[0].Name(), "echo")
	}
}

func TestHandleRunError(t *testing.T) {
	runErr := errors.New("run failed")
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return nil, runErr
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = tool.Tool().Handle(context.Background(), `{"name":"agent","description":"a desc","input":"do work"}`)
	if !errors.Is(err, runErr) {
		t.Fatalf("Handle() error = %v, want %v", err, runErr)
	}
}

func TestToolMiddlewareOrder(t *testing.T) {
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return &llm.Result{Message: &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text("ok")}}}, nil
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	steps := make([]string, 0, 4)
	m1 := func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			steps = append(steps, "m1-before")
			res, err := next(ctx, arguments)
			steps = append(steps, "m1-after")
			return res, err
		}
	}
	m2 := func(_ *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			steps = append(steps, "m2-before")
			res, err := next(ctx, arguments)
			steps = append(steps, "m2-after")
			return res, err
		}
	}

	tool.Tool().Use(m1, m2)
	_, err = tool.Tool().Handle(context.Background(), `{"name":"agent","description":"a desc","input":"go"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	want := []string{"m1-before", "m2-before", "m2-after", "m1-after"}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("middleware steps = %v, want %v", steps, want)
	}
}

func TestToolMiddlewareShortCircuit(t *testing.T) {
	tool, err := New(stubLLM{execute: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		return nil, errors.New("should not be called")
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tool.Tool().Use(func(_ *llm.Tool, _ llm.ToolHandler) llm.ToolHandler {
		return func(_ context.Context, _ string) (any, error) {
			return &Output{Output: "blocked"}, nil
		}
	})

	res, err := tool.Tool().Handle(context.Background(), `{"name":"agent","description":"a desc","input":"go"}`)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	out, ok := res.(*Output)
	if !ok {
		t.Fatalf("Handle() result type = %T, want *Output", res)
	}
	if out.Output != "blocked" {
		t.Fatalf("output = %q, want %q", out.Output, "blocked")
	}
}
