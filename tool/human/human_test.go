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

package human

import (
	"context"
	"errors"
	"testing"
)

func TestNewRequiresInteractor(t *testing.T) {
	t.Parallel()

	_, err := New()
	if !errors.Is(err, ErrInteractorRequired) {
		t.Fatalf("expected ErrInteractorRequired, got %v", err)
	}
}

func TestAskNilInput(t *testing.T) {
	t.Parallel()

	tool, err := New(WithInteractor(func(context.Context, *Input) (map[string]Answer, error) {
		return map[string]Answer{}, nil
	}))
	if err != nil {
		t.Fatalf("unexpected error creating tool: %v", err)
	}

	_, err = tool.ask(context.Background(), nil)
	if !errors.Is(err, ErrNilInput) {
		t.Fatalf("expected ErrNilInput, got %v", err)
	}
}

func TestAskReturnsStructuredAnswers(t *testing.T) {
	t.Parallel()

	input := &Input{Questions: []Question{
		{
			Header:      "Approve",
			Question:    "Apply this action?",
			MultiSelect: false,
			Options: []Choice{
				{Label: "Approve", Description: "Apply the action"},
				{Label: "Skip", Description: "Skip this action"},
			},
		},
	}}

	tool, err := New(WithInteractor(func(context.Context, *Input) (map[string]Answer, error) {
		return map[string]Answer{
			"Approve": {
				Selections: []string{"Approve"},
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("unexpected error creating tool: %v", err)
	}

	out, err := tool.ask(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected ask error: %v", err)
	}

	answer, ok := out.Answers["Approve"]
	if !ok {
		t.Fatalf("expected answer for Approve header")
	}

	if len(answer.Selections) != 1 || answer.Selections[0] != "Approve" {
		t.Fatalf("unexpected selections: %#v", answer.Selections)
	}
}

func TestAskSupportsMultiSelect(t *testing.T) {
	t.Parallel()

	input := &Input{Questions: []Question{
		{
			Header:      "Features",
			Question:    "Which features do you want to enable?",
			MultiSelect: true,
			Options: []Choice{
				{Label: "Lint", Description: "Enable lint checks"},
				{Label: "Tests", Description: "Enable test checks"},
				{Label: "Coverage", Description: "Enable coverage checks"},
			},
		},
	}}

	tool, err := New(WithInteractor(func(context.Context, *Input) (map[string]Answer, error) {
		return map[string]Answer{
			"Features": {
				Selections: []string{"Lint", "Tests"},
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("unexpected error creating tool: %v", err)
	}

	out, err := tool.ask(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected ask error: %v", err)
	}

	if got := len(out.Answers["Features"].Selections); got != 2 {
		t.Fatalf("expected 2 selections, got %d", got)
	}
}

func TestAskPropagatesInteractorFailure(t *testing.T) {
	t.Parallel()

	interactorErr := errors.New("backend unavailable")

	tool, err := New(WithInteractor(func(context.Context, *Input) (map[string]Answer, error) {
		return nil, interactorErr
	}))
	if err != nil {
		t.Fatalf("unexpected error creating tool: %v", err)
	}

	_, err = tool.ask(context.Background(), &Input{Questions: []Question{
		{
			Header:      "Decision",
			Question:    "Proceed?",
			MultiSelect: false,
			Options:     []Choice{{Label: "Yes", Description: "Proceed"}, {Label: "No", Description: "Do not proceed"}},
		},
	}})
	if !errors.Is(err, ErrInteractionFailed) {
		t.Fatalf("expected ErrInteractionFailed, got %v", err)
	}

	if !errors.Is(err, interactorErr) {
		t.Fatalf("expected wrapped interactor error, got %v", err)
	}
}

func TestAskRejectsInvalidAnswerSelection(t *testing.T) {
	t.Parallel()

	tool, err := New(WithInteractor(func(context.Context, *Input) (map[string]Answer, error) {
		return map[string]Answer{
			"Decision": {
				Selections: []string{"Maybe"},
			},
		}, nil
	}))
	if err != nil {
		t.Fatalf("unexpected error creating tool: %v", err)
	}

	_, err = tool.ask(context.Background(), &Input{Questions: []Question{
		{
			Header:      "Decision",
			Question:    "Proceed?",
			MultiSelect: false,
			Options:     []Choice{{Label: "Yes", Description: "Proceed"}, {Label: "No", Description: "Do not proceed"}},
		},
	}})
	if !errors.Is(err, ErrInvalidOptionSelection) {
		t.Fatalf("expected ErrInvalidOptionSelection, got %v", err)
	}
}
