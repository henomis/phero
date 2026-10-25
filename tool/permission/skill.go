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
	"regexp"
	"slices"
	"strings"
)

// launchers are commands that run another command, or arbitrary code, given
// as their arguments: a grant for "sudo *" or "python *" is a grant for
// everything. A version suffix is ignored ("python3.12" is "python").
const launchers = "sh bash zsh dash ksh mksh ash tcsh csh fish pwsh powershell busybox " +
	"env sudo su doas eval exec xargs nohup timeout nice time command builtin chroot setsid stdbuf " +
	"script watch strace ltrace gdb " +
	"python node perl ruby php lua awk gawk mawk find"

// subcommandTools take global options before their subcommand that can run
// any command ("git -c alias.x='!sh'"): a grant must name the subcommand.
const subcommandTools = "git"

// plainCommand matches a command name the shell runs as written: no quotes,
// escapes or expansions.
var plainCommand = regexp.MustCompile(`^[A-Za-z0-9_./+-]+$`)

// SkillGrants splits a skill's allowed-tools into the rules safe to grant and
// the ones refused. A skill comes from files a user may not have written (a
// cloned project's skills), so it may not pre-approve bash at large: a rule for
// bash must start with a fixed, plain command that does not run other
// commands, such as "Bash(git status:*)". Refused are bash with no pattern or
// with a pattern that starts with a wildcard or a quoted or escaped name
// ("\sh"), a tool-name pattern that covers bash ("*", "b*"), a command such as
// sh, sudo, env or python, and git without a subcommand.
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
	if len(fields) == 0 || !plainCommand.MatchString(fields[0]) {
		return true
	}

	command := path.Base(fields[0])
	if unversioned := strings.TrimRight(command, "0123456789."); unversioned != "" {
		command = unversioned
	}

	if slices.Contains(strings.Fields(subcommandTools), command) &&
		(len(fields) < 2 || strings.HasPrefix(fields[1], "-") || strings.Contains(fields[1], "*")) {
		return true
	}

	return slices.Contains(strings.Fields(launchers), command)
}
