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

package openai

import "errors"

// ErrEmptyResponse is returned by Execute when the API responds with no choices.
//
// This can happen due to content filtering, quota exhaustion, or certain stop
// sequences being triggered before any content is generated.
var ErrEmptyResponse = errors.New("openai: response contained no choices")

// ErrTranscriptionInputRequired is returned when a transcription request does not
// provide either a file path or a reader.
var ErrTranscriptionInputRequired = errors.New("openai: transcription input is required")

// ErrSpeechInputRequired is returned when a speech synthesis request does not
// provide any input text.
var ErrSpeechInputRequired = errors.New("openai: speech input text is required")
