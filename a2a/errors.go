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

package a2a

import "errors"

// ErrAgentRequired is returned when a nil agent is passed to New.
var ErrAgentRequired = errors.New("a2a: agent is required")

// ErrBaseURLRequired is returned when an empty public base URL is passed to New.
var ErrBaseURLRequired = errors.New("a2a: base URL is required")

// ErrURLRequired is returned when an empty base URL is passed to NewClient.
var ErrURLRequired = errors.New("a2a: base URL is required")

// ErrInvalidBaseURL is returned when the provided base URL is not a
// well-formed absolute URL (must have a scheme and host).
var ErrInvalidBaseURL = errors.New("a2a: base URL must be a well-formed absolute URL with a scheme and host")

// ErrNoTextContent is returned when a remote A2A response contains no text part.
var ErrNoTextContent = errors.New("a2a: response contains no text content")

// ErrEmptyResponse is returned when a remote A2A response is nil.
var ErrEmptyResponse = errors.New("a2a: empty response from remote agent")
