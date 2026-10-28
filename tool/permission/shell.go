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
//     part of another, or its text appears anywhere, even quoted: bash runs
//     "$(...)" found in an array subscript during arithmetic, or in ${x@P};
//   - it evaluates arithmetic or indirection ($((...)), ((...)), [[ ... ]],
//     let, ${a[i]}, ${a:i}, ${!a}, ${a@op}), which can run such text from a
//     variable;
//   - a statement only assigns variables, or declares them (export, local,
//     declare, readonly), which changes what later commands do (PATH=.);
//   - a command's name is not a plain word ("$CMD", a quoted or escaped
//     name such as "\sh"), or names a function the line defines;
//   - a redirection writes a file ("cat a > ~/.bashrc"); "2>&1", ">&-",
//     ">/dev/null", input redirections and heredocs write none.
//
// The commands are returned in both cases, for the deny rules to see.
func splitShell(line string) ([]string, bool) {
	file, err := syntax.NewParser().Parse(strings.NewReader(line), "")
	if err != nil {
		return splitOperators(line), false
	}

	scan := &shellScan{
		safe:    !strings.ContainsAny(line, "`") && !containsAny(line, "$(", "<(", ">("),
		funcs:   map[string]bool{},
		printer: syntax.NewPrinter(syntax.SingleLine(true)),
	}

	syntax.Walk(file, scan.visit)

	return scan.commands, scan.safe
}

// shellScan collects a parsed line's simple commands and whether the line
// can be judged from them (see splitShell).
type shellScan struct {
	commands []string
	safe     bool
	funcs    map[string]bool
	printer  *syntax.Printer
}

// visit inspects one node of the syntax tree; it always walks on.
func (s *shellScan) visit(node syntax.Node) bool {
	switch n := node.(type) {
	case *syntax.CmdSubst, *syntax.ProcSubst, *syntax.ArithmExp, *syntax.ArithmCmd,
		*syntax.TestClause, *syntax.LetClause, *syntax.DeclClause, *syntax.TestDecl:
		s.safe = false
	case *syntax.ParamExp:
		if n.Index != nil || n.Slice != nil || n.Excl || (n.Exp != nil && n.Exp.Op == syntax.OtherParamOps) {
			s.safe = false
		}
	case *syntax.FuncDecl:
		s.funcs[n.Name.Value] = true
	case *syntax.Stmt:
		for _, r := range n.Redirs {
			s.safe = s.safe && harmlessRedirect(r)
		}
	case *syntax.CallExpr:
		s.call(n)
	}

	return true
}

// call records a simple command, printed in normal form.
func (s *shellScan) call(n *syntax.CallExpr) {
	if len(n.Args) == 0 {
		s.safe = false // assignments only: they change what later commands do
		return
	}

	var b bytes.Buffer
	if s.printer.Print(&b, n) != nil {
		s.safe = false
		return
	}

	s.commands = append(s.commands, strings.TrimSpace(b.String()))

	if name := n.Args[0].Lit(); name == "" || strings.Contains(name, `\`) || s.funcs[name] {
		s.safe = false
	}
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

// containsAny reports whether s contains any of subs.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}

	return false
}
