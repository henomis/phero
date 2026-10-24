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

import "strings"

// shellOperators separate the commands of a shell command line. Longer
// operators come first so "&&" is not read as two "&".
var shellOperators = []string{"&&", "||", ";", "|", "&", "\n"}

// splitShell returns the commands of a shell command line, split on its
// operators. It reports false when the line runs commands it cannot see
// through: command or process substitution.
func splitShell(line string) ([]string, bool) {
	for _, sub := range []string{"$(", "`", "<(", ">("} {
		if strings.Contains(line, sub) {
			return nil, false
		}
	}

	parts := []string{line}

	for _, op := range shellOperators {
		var next []string

		for _, p := range parts {
			next = append(next, strings.Split(p, op)...)
		}

		parts = next
	}

	commands := make([]string, 0, len(parts))

	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			commands = append(commands, p)
		}
	}

	return commands, true
}
