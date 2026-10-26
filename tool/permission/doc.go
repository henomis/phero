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

// Package permission decides whether an agent may run a tool call: allowed,
// denied, or asked of a human first.
//
// A Policy holds rules written as a tool name, optionally followed by a
// pattern on the call's subject in parentheses:
//
//	bash(go test *)      a bash command starting with "go test "
//	edit(/repo/**)       an edit of a file under /repo
//	mcp_github_*         any tool whose name starts with mcp_github_
//	write                any write
//
// Tool names match case-insensitively. In a pattern, * matches any run of
// characters, slashes included, and a trailing ":*" is read as "*" (the
// prefix form some agents use, as in "Bash(git:*)"). The subject is the
// argument that says what a call touches: the command for bash, the file path
// for read, write, edit and multi_edit, the search path for glob and grep, the
// URL for web_fetch; WithSubject sets it for other tools. A subject argument
// named file_path or path is a file path, for any tool. A rule with no
// pattern matches every call of its tool.
//
// File paths, in subjects and in rule patterns, are matched as the file tools
// open them: cleaned, made absolute against the working directory
// (WithWorkingDir, the process's by default), with a leading ~ read as the
// home directory, and resolved through symlinks,
// so "/repo/src/../secrets/k" meets read(/repo/secrets/*). Deny and ask rules
// also match the path as written, before its symlinks are resolved, so
// read(/repo/*/.env) still covers a symlinked package. Arguments that
// give the subject argument twice, in any letter case, are denied: the tool's
// handler would decode the last one, which the rules might not have seen.
//
// A call is denied when a deny rule matches, else asked when an ask rule
// matches, else allowed when an allow rule matches, else given the default
// decision (Ask unless WithDefault says otherwise).
//
// A bash command line is parsed as the shell would (mvdan.cc/sh), and every
// simple command in it must be allowed on its own: those joined by &&, ||, ;,
// | and &, and those inside subshells, if/for/while bodies, function bodies
// and substitutions. So "go test ./... && rm -rf /" does not pass
// bash(go test *), quoted operators do not split ("echo 'a && b'" is one
// command), and commands are matched in a normal form, single-spaced. An
// assignment before a command is part of it: "CGO_ENABLED=0 go test" does
// not pass bash(go test *), because a variable can change what an allowed
// program runs (GOFLAGS=-toolexec=..., LD_PRELOAD); a rule that names it
// does, as bash(CGO_ENABLED=0 go test *). A line
// is asked, unless a deny rule matches one of its commands, when it does not
// parse; holds the text of a command or process substitution anywhere, even
// quoted (bash runs "$(...)" from an array subscript or ${x@P}); evaluates
// arithmetic or indirection ($((...)), ((...)), [[ ... ]], let, ${a[i]},
// ${a:i}, ${!a}, ${a@op}); only assigns or declares variables (PATH=.,
// export); names its command with something other than a plain word ("$CMD",
// a function it defines); or redirects output to a file ("cat a >> ~/.bashrc"
// does not pass bash(cat *)). "2>&1", ">&-", ">/dev/null", input redirections
// and heredocs write no file.
//
// Bash rules are advisory, not a security boundary. They match the command
// text, so a deny rule misses the same program written another way:
// bash(rm -rf *) does not match "rm  -rf", "rm -fr", "/bin/rm -rf" or
// "\rm -rf", nor rm run from inside "sh -c", a script or a Makefile. Use deny
// rules to stop mistakes, allow rules for commands safe to run unasked, and a
// sandbox (bash.NewBubblewrapRunner) where a restriction must hold.
//
// A glob or grep call with no path searches the working directory, so a rule
// such as glob(/repo/secrets*) does not cover it: deny the directory itself
// (grep(/repo)) or keep secrets outside the working directory.
//
// AllowForRun grants rules for the current agent run only (llm.Run), as for
// the tools a skill pre-approves; SkillGrants leaves out the grants too broad
// for a skill, such as bare bash.
//
// Asking goes through an Asker, one question at a time even when the agent
// runs tool calls in parallel. An answer of AllowAlways allows the same subject
// for the rest of the Policy's life, or until Forget drops it; Always lists
// those answers. A denied call returns a *DeniedError,
// which the agent reports to the model as a failed tool call.
//
//	policy, err := permission.New(
//		permission.WithAllow("read", "grep", "glob", "bash(go test *)", "bash(go build *)"),
//		permission.WithDeny("bash(git push *)"), // a guard against mistakes, not a boundary
//		permission.WithAsker(askInTerminal),
//	)
//	policy.Apply(readTool.Tool(), bashTool.Tool(), editTool.Tool())
package permission
