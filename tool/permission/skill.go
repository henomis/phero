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
	"path"
	"slices"
	"strings"
)

// launchers are commands that run another command, or arbitrary code, given
// as their arguments: a grant for "sudo *" or "python *" is a grant for
// everything.
const launchers = "sh bash zsh dash ksh fish env sudo su doas eval exec xargs nohup timeout nice time command " +
	"python python3 node perl ruby php awk find"

// SkillGrants splits a skill's allowed-tools into the rules safe to grant and
// the ones refused. A skill comes from files a user may not have written (a
// cloned project's skills), so it may not pre-approve bash at large: a rule for
// bash must start with a fixed command that does not run other commands, such
// as "Bash(git status:*)". Refused are bash with no pattern or with a pattern
// that starts with a wildcard, a tool-name pattern that covers bash ("*",
// "b*"), and a command such as sh, sudo, env or python.
//
//	granted, refused := permission.SkillGrants(s.Tools())
//	_ = policy.AllowForRun(ctx, granted...) // log refused
func SkillGrants(specs []string) (granted, refused []string) {
	for _, spec := range specs {
		if broadBashGrant(spec) {
			refused = append(refused, spec)
		} else {
			granted = append(granted, spec)
		}
	}

	return granted, refused
}

// broadBashGrant reports whether spec allows bash commands beyond one fixed
// command.
func broadBashGrant(spec string) bool {
	name, pattern, hasPattern := strings.Cut(strings.TrimSpace(spec), "(")

	name = strings.TrimSpace(name)
	if !compileWildcard("(?i)", name).MatchString(bashTool) {
		return false
	}

	if strings.Contains(name, "*") || !hasPattern {
		return true
	}

	pattern = strings.TrimSuffix(strings.TrimSuffix(pattern, ")"), ":*")

	fields := strings.Fields(pattern)
	if len(fields) == 0 || strings.Contains(fields[0], "*") {
		return true
	}

	return slices.Contains(strings.Fields(launchers), path.Base(fields[0]))
}
