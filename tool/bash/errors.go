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

package bash

import "errors"

// ErrNilInput is returned when a nil Input is passed to the tool handler.
var ErrNilInput = errors.New("bash: nil input")

// ErrCommandRequired is returned when the Command field of the input is empty.
var ErrCommandRequired = errors.New("bash: command is required")

// ErrCommandBlocked is returned when the command matches a blocklist pattern.
var ErrCommandBlocked = errors.New("bash: command blocked by policy")

// ErrCommandNotAllowed is returned when an allowlist is configured and the
// command does not match any allowed pattern.
var ErrCommandNotAllowed = errors.New("bash: command not in allowlist")
