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

package permission

import (
	"errors"
	"fmt"
)

// ErrDenied is matched with errors.Is by the error of a denied tool call.
var ErrDenied = errors.New("tool call denied")

// ErrInvalidRule is returned by New for a rule that cannot be parsed.
var ErrInvalidRule = errors.New("invalid permission rule")

// DeniedError is returned for a tool call the Policy denied.
type DeniedError struct {
	// Tool is the tool's name.
	Tool string
	// Subject is what the call touches, such as the command or the file path;
	// empty for a tool without one.
	Subject string
	// Reason says why: the deny rule that matched, the user, or no Asker.
	Reason string
}

func (e *DeniedError) Error() string {
	if e.Subject == "" {
		return fmt.Sprintf("permission denied for %s: %s", e.Tool, e.Reason)
	}

	return fmt.Sprintf("permission denied for %s(%s): %s", e.Tool, e.Subject, e.Reason)
}

// Is reports whether target is ErrDenied.
func (e *DeniedError) Is(target error) bool {
	return target == ErrDenied
}
