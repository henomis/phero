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

import "errors"

// ErrNilInput is returned when the tool handler receives a nil input.
var ErrNilInput = errors.New("nil input")

// ErrInteractorRequired is returned when the interactor callback is not configured.
var ErrInteractorRequired = errors.New("interactor required")

// ErrInteractionFailed is returned when answer collection fails.
var ErrInteractionFailed = errors.New("user interaction failed")

// ErrQuestionsRequired is returned when no questions are provided.
var ErrQuestionsRequired = errors.New("at least one question is required")

// ErrTooManyQuestions is returned when the number of questions exceeds the supported limit.
var ErrTooManyQuestions = errors.New("too many questions")

// ErrHeaderRequired is returned when a question header is empty.
var ErrHeaderRequired = errors.New("question header required")

// ErrHeaderTooLong is returned when a question header exceeds the maximum length.
var ErrHeaderTooLong = errors.New("question header too long")

// ErrDuplicateQuestionHeader is returned when duplicated headers are provided.
var ErrDuplicateQuestionHeader = errors.New("duplicate question header")

// ErrQuestionTextRequired is returned when a question text is empty.
var ErrQuestionTextRequired = errors.New("question text required")

// ErrQuestionMustEndWithQuestionMark is returned when a question text is not phrased as a question.
var ErrQuestionMustEndWithQuestionMark = errors.New("question must end with '?'")

// ErrInvalidOptionCount is returned when a question has an unsupported number of options.
var ErrInvalidOptionCount = errors.New("question must define between 2 and 4 options")

// ErrOptionLabelRequired is returned when an option label is empty.
var ErrOptionLabelRequired = errors.New("option label required")

// ErrOptionLabelTooLong is returned when an option label exceeds the allowed word count.
var ErrOptionLabelTooLong = errors.New("option label must contain between 1 and 5 words")

// ErrOptionDescriptionRequired is returned when an option description is empty.
var ErrOptionDescriptionRequired = errors.New("option description required")

// ErrInvalidAnswerHeader is returned when answers include unknown question headers.
var ErrInvalidAnswerHeader = errors.New("invalid answer header")

// ErrInvalidOptionSelection is returned when an answer includes an unknown option label.
var ErrInvalidOptionSelection = errors.New("invalid option selection")

// ErrMultipleSelectionsNotAllowed is returned when multiple answers are returned for a single-select question.
var ErrMultipleSelectionsNotAllowed = errors.New("multiple selections not allowed")
