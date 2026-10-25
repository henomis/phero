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
	"bytes"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// shellOperators separate the commands of a shell command line. Longer
// operators come first so "&&" is not read as two "&". They split a line the
// parser rejects, to find what deny rules can still see.
var shellOperators = []string{"&&", "||", ";", "|", "&", "\n"}

// splitShell parses a shell command line and returns its simple commands,
// printed in a normal form (single spaces, the words as written), those inside
// subshells, compound commands, function bodies and substitutions included. It
// reports false when the line cannot be judged from them:
//   - it does not parse;
//   - a command or process substitution runs a command whose output becomes
//     part of another;
//   - a command's name is not a plain word ("$CMD", a quoted name), or names a
//     function the line defines;
//   - a redirection writes a file ("cat a > ~/.bashrc"); "2>&1", ">&-",
//     ">/dev/null", input redirections and heredocs write none.
//
// The commands are returned in both cases, for the deny rules to see.
func splitShell(line string) ([]string, bool) {
	file, err := syntax.NewParser().Parse(strings.NewReader(line), "")
	if err != nil {
		return splitOperators(line), false
	}

	var (
		commands []string
		safe     = true
		funcs    = map[string]bool{}
		printer  = syntax.NewPrinter(syntax.SingleLine(true))
	)

	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			safe = false
		case *syntax.FuncDecl:
			funcs[n.Name.Value] = true
		case *syntax.Stmt:
			for _, r := range n.Redirs {
				safe = safe && harmlessRedirect(r)
			}
		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				return true // assignments only
			}

			var b bytes.Buffer
			if printer.Print(&b, n) != nil {
				safe = false
				return true
			}

			commands = append(commands, strings.TrimSpace(b.String()))

			if name := n.Args[0].Lit(); name == "" || funcs[name] {
				safe = false
			}
		}

		return true
	})

	return commands, safe
}

// harmlessRedirect reports whether r writes no file: an input redirection, a
// heredoc or here-string, a descriptor duplicated or closed, or output sent to
// /dev/null.
func harmlessRedirect(r *syntax.Redirect) bool {
	target := ""
	if r.Word != nil {
		target = r.Word.Lit()
	}

	//nolint:exhaustive // every other operator writes a file
	switch r.Op {
	case syntax.RdrIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc, syntax.DplIn:
		return true
	case syntax.DplOut:
		return target == "-" || (target != "" && strings.Trim(target, "0123456789") == "")
	case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll, syntax.RdrClob:
		return target == "/dev/null"
	default:
		return false
	}
}

// splitOperators splits a command line on its operators, with no check of
// what it runs.
func splitOperators(line string) []string {
	parts := []string{line}

	for _, op := range shellOperators {
		next := make([]string, 0, len(parts))

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

	return commands
}
