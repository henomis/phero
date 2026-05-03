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
	"strings"
	"unicode/utf8"

	"github.com/henomis/phero/llm"
)

const (
	toolName        = "user_interaction"
	toolDescription = "Use this tool to ask the user one or more structured questions and collect their answers."

	minQuestions = 1
	maxQuestions = 4
	minOptions   = 2
	maxOptions   = 4

	maxHeaderLen       = 12
	maxOptionLabelWord = 5
)

// Input defines the user interaction request.
type Input struct {
	Questions []Question `json:"questions" jsonschema:"description=Questions to ask the user (1-4 questions)"`
}

// Question defines a single user-facing question.
type Question struct {
	Header      string   `json:"header" jsonschema:"description=Very short label (max 12 chars)"`
	Question    string   `json:"question" jsonschema:"description=The complete question to ask the user"`
	MultiSelect bool     `json:"multiSelect" jsonschema:"description=Allow multiple selections"`
	Options     []Choice `json:"options" jsonschema:"description=Available choices (2-4 options)"`
}

// Choice defines a single selectable option for a question.
type Choice struct {
	Label       string `json:"label" jsonschema:"description=Display text for this option (1-5 words)"`
	Description string `json:"description" jsonschema:"description=Explanation of this option"`
}

// Answer defines user selections for a question.
type Answer struct {
	Selections []string `json:"selections,omitempty" jsonschema:"description=Option labels selected by the user"`
	Other      string   `json:"other,omitempty" jsonschema:"description=Optional custom free-text answer"`
}

// Output is the structured result returned by the tool.
type Output struct {
	Answers map[string]Answer `json:"answers" jsonschema:"description=Collected user answers keyed by question header"`
}

// Interactor is the callback used to present questions and collect answers.
type Interactor func(ctx context.Context, input *Input) (map[string]Answer, error)

// Tool wraps the user interaction tool.
type Tool struct {
	tool       *llm.Tool
	interactor Interactor
}

// Option configures a Tool created by New.
type Option func(*Tool)

// WithInteractor overrides the callback used to collect user answers.
func WithInteractor(interactor Interactor) Option {
	return func(t *Tool) {
		t.interactor = interactor
	}
}

// New creates a new user interaction tool.
func New(opts ...Option) (*Tool, error) {
	t := &Tool{}

	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}

	if t.interactor == nil {
		return nil, ErrInteractorRequired
	}

	tool, err := llm.NewTool(
		toolName,
		toolDescription,
		t.ask,
	)
	if err != nil {
		return nil, err
	}

	t.tool = tool

	return t, nil
}

// Tool returns the llm.Tool representation of the user interaction tool.
func (h *Tool) Tool() *llm.Tool {
	return h.tool
}

func (h *Tool) ask(ctx context.Context, input *Input) (*Output, error) {
	if input == nil {
		return nil, ErrNilInput
	}
	if err := validateInput(input); err != nil {
		return nil, err
	}

	answers, err := h.interactor(ctx, input)
	if err != nil {
		return nil, errors.Join(ErrInteractionFailed, err)
	}

	if answers == nil {
		answers = map[string]Answer{}
	}

	if err := validateAnswers(input.Questions, answers); err != nil {
		return nil, err
	}

	return &Output{Answers: answers}, nil
}

func validateInput(input *Input) error {
	questions := input.Questions
	if len(questions) < minQuestions {
		return ErrQuestionsRequired
	}
	if len(questions) > maxQuestions {
		return ErrTooManyQuestions
	}

	seenHeaders := map[string]struct{}{}
	for _, q := range questions {
		header := strings.TrimSpace(q.Header)
		if header == "" {
			return ErrHeaderRequired
		}
		if utf8.RuneCountInString(header) > maxHeaderLen {
			return ErrHeaderTooLong
		}

		normalizedHeader := strings.ToLower(header)
		if _, exists := seenHeaders[normalizedHeader]; exists {
			return ErrDuplicateQuestionHeader
		}
		seenHeaders[normalizedHeader] = struct{}{}

		questionText := strings.TrimSpace(q.Question)
		if questionText == "" {
			return ErrQuestionTextRequired
		}
		if !strings.HasSuffix(questionText, "?") {
			return ErrQuestionMustEndWithQuestionMark
		}

		if len(q.Options) < minOptions || len(q.Options) > maxOptions {
			return ErrInvalidOptionCount
		}

		for _, option := range q.Options {
			label := strings.TrimSpace(option.Label)
			if label == "" {
				return ErrOptionLabelRequired
			}

			words := strings.Fields(label)
			if len(words) == 0 || len(words) > maxOptionLabelWord {
				return ErrOptionLabelTooLong
			}

			if strings.TrimSpace(option.Description) == "" {
				return ErrOptionDescriptionRequired
			}
		}
	}

	return nil
}

func validateAnswers(questions []Question, answers map[string]Answer) error {
	allowed := map[string]Question{}
	for _, q := range questions {
		allowed[strings.ToLower(strings.TrimSpace(q.Header))] = q
	}

	for key, answer := range answers {
		q, exists := allowed[strings.ToLower(strings.TrimSpace(key))]
		if !exists {
			return ErrInvalidAnswerHeader
		}

		if !q.MultiSelect && len(answer.Selections) > 1 {
			return ErrMultipleSelectionsNotAllowed
		}

		optionLabels := map[string]struct{}{}
		for _, option := range q.Options {
			optionLabels[strings.ToLower(strings.TrimSpace(option.Label))] = struct{}{}
		}

		for _, selection := range answer.Selections {
			if _, ok := optionLabels[strings.ToLower(strings.TrimSpace(selection))]; !ok {
				return ErrInvalidOptionSelection
			}
		}
	}

	return nil
}
