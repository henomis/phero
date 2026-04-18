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

package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/henomis/phero/llm"
)

var errGuard = errors.New("guard blocked")

// passMessageGuard is a MessageGuard that always passes.
func passMessageGuard(_ context.Context, _ []llm.Message) error { return nil }

// blockMessageGuard is a MessageGuard that always blocks.
func blockMessageGuard(_ context.Context, _ []llm.Message) error { return errGuard }

// passResultGuard is a ResultGuard that always passes.
func passResultGuard(_ context.Context, _ *llm.Result) error { return nil }

// blockResultGuard is a ResultGuard that always blocks.
func blockResultGuard(_ context.Context, _ *llm.Result) error { return errGuard }

// TestGuardrails_NoGuards verifies that with no guards the result is passed through unchanged.
func TestGuardrails_NoGuards(t *testing.T) {
	mw := NewGuardrails()
	client := llm.Use(okLLM("hello"), mw)

	result, err := client.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Message.TextContent() != "hello" {
		t.Fatalf("got %q, want %q", result.Message.TextContent(), "hello")
	}
}

// TestGuardrails_MessageGuard_Pass verifies that a passing input guard does not block.
func TestGuardrails_MessageGuard_Pass(t *testing.T) {
	mw := NewGuardrails(WithMessageGuard("allow-all", passMessageGuard))
	client := llm.Use(okLLM("ok"), mw)

	_, err := client.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// TestGuardrails_MessageGuard_Block verifies that a failing input guard stops execution.
func TestGuardrails_MessageGuard_Block(t *testing.T) {
	mw := NewGuardrails(WithMessageGuard("block", blockMessageGuard))
	reached := false
	inner := &stubLLM{fn: func(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
		reached = true
		return nil, nil
	}}
	client := llm.Use(inner, mw)

	_, err := client.Execute(context.Background(), nil, nil)

	var gErr *GuardrailError
	if !errors.As(err, &gErr) {
		t.Fatalf("got %T %v, want GuardrailError", err, err)
	}
	if gErr.Stage != "input" {
		t.Fatalf("Stage=%q, want %q", gErr.Stage, "input")
	}
	if gErr.Name != "block" {
		t.Fatalf("Name=%q, want %q", gErr.Name, "block")
	}
	if !errors.Is(err, errGuard) {
		t.Fatal("Unwrap should yield errGuard")
	}
	if reached {
		t.Fatal("inner LLM should not have been called")
	}
}

// TestGuardrails_ResultGuard_Pass verifies that a passing output guard lets the result through.
func TestGuardrails_ResultGuard_Pass(t *testing.T) {
	mw := NewGuardrails(WithResultGuard("allow-all", passResultGuard))
	client := llm.Use(okLLM("ok"), mw)

	result, err := client.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Message.TextContent() != "ok" {
		t.Fatalf("got %q, want %q", result.Message.TextContent(), "ok")
	}
}

// TestGuardrails_ResultGuard_Block verifies that a failing output guard rejects the result.
func TestGuardrails_ResultGuard_Block(t *testing.T) {
	mw := NewGuardrails(WithResultGuard("block", blockResultGuard))
	client := llm.Use(okLLM("ok"), mw)

	_, err := client.Execute(context.Background(), nil, nil)

	var gErr *GuardrailError
	if !errors.As(err, &gErr) {
		t.Fatalf("got %T %v, want GuardrailError", err, err)
	}
	if gErr.Stage != "output" {
		t.Fatalf("Stage=%q, want %q", gErr.Stage, "output")
	}
	if gErr.Name != "block" {
		t.Fatalf("Name=%q, want %q", gErr.Name, "block")
	}
	if !errors.Is(err, errGuard) {
		t.Fatal("Unwrap should yield errGuard")
	}
}

// TestGuardrails_InnerError verifies that inner LLM errors bypass result guards.
func TestGuardrails_InnerError(t *testing.T) {
	sentinel := errors.New("inner error")
	mw := NewGuardrails(WithResultGuard("block", blockResultGuard))
	client := llm.Use(errLLM(sentinel), mw)

	_, err := client.Execute(context.Background(), nil, nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("got %v, want sentinel inner error", err)
	}
}

// TestGuardrails_GuardOrder verifies that guards are checked in the order added.
func TestGuardrails_GuardOrder(t *testing.T) {
	order := []string{}

	guard := func(name string) MessageGuard {
		return func(_ context.Context, _ []llm.Message) error {
			order = append(order, name)
			return nil
		}
	}

	mw := NewGuardrails(
		WithMessageGuard("first", guard("first")),
		WithMessageGuard("second", guard("second")),
	)
	client := llm.Use(okLLM("ok"), mw)

	_, err := client.Execute(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("guard order: %v", order)
	}
}
