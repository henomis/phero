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

package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

type weatherReport struct {
	City  string `json:"city"`
	TempC int    `json:"tempC"`
}

func TestRunTyped_CapturesFinalAnswer(t *testing.T) {
	stub := &stubLLM{
		responses: []*llm.Result{
			toolCallResult("final_answer", "c1", `{"city":"Paris","tempC":21}`),
			textResult("all done"),
		},
		errs: []error{nil, nil},
	}
	a := mustNew(t, stub, "agent", "report the weather")

	got, result, err := agent.RunTyped[weatherReport](context.Background(), a, llm.Text("weather in Paris?"))
	if err != nil {
		t.Fatalf("RunTyped: %v", err)
	}
	if got.City != "Paris" || got.TempC != 21 {
		t.Fatalf("typed result = %+v, want {Paris 21}", got)
	}
	if result == nil {
		t.Fatal("expected non-nil *Result")
	}
}

func TestRunTyped_NoToolCallReturnsError(t *testing.T) {
	stub := &stubLLM{
		responses: []*llm.Result{textResult("I answered in plain text")},
		errs:      []error{nil},
	}
	a := mustNew(t, stub, "agent", "desc")

	_, _, err := agent.RunTyped[weatherReport](context.Background(), a, llm.Text("go"))
	if !errors.Is(err, agent.ErrNoStructuredOutput) {
		t.Fatalf("err = %v, want ErrNoStructuredOutput", err)
	}
}

func TestRunTyped_DoesNotMutateAgent(t *testing.T) {
	stub := &stubLLM{
		responses: []*llm.Result{
			toolCallResult("final_answer", "c1", `{"city":"Rome","tempC":30}`),
			textResult("done"),
		},
		errs: []error{nil, nil},
	}
	a := mustNew(t, stub, "agent", "desc")

	if _, _, err := agent.RunTyped[weatherReport](context.Background(), a, llm.Text("go")); err != nil {
		t.Fatalf("RunTyped: %v", err)
	}

	// The synthetic final_answer tool must not have been added to the original agent,
	// so a subsequent AddTool of that name must succeed.
	tool, err := llm.NewTool("final_answer", "", func(_ context.Context, _ *struct{}) (string, error) {
		return "", nil
	})
	if err != nil {
		t.Fatalf("NewTool: %v", err)
	}
	if err := a.AddTool(tool); err != nil {
		t.Fatalf("agent was mutated: final_answer already present: %v", err)
	}
}
