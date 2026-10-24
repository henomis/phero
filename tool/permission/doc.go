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
// In a pattern, * matches any run of characters, slashes included. The subject
// is the argument that says what a call touches: the command for bash, the
// file path for read, write and edit, the search path for glob and grep;
// WithSubject sets it for other tools. A rule with no pattern matches every
// call of its tool.
//
// A call is denied when a deny rule matches, else asked when an ask rule
// matches, else allowed when an allow rule matches, else given the default
// decision (Ask unless WithDefault says otherwise).
//
// A bash command is split on &&, ||, ;, |, & and newlines, and every part must
// be allowed on its own, so "go test ./... && rm -rf /" does not pass
// bash(go test *). A command with $(...), backticks or process substitution is
// always asked. The split does not understand quotes: a quoted operator splits
// too, which can only make a command asked, never allowed.
//
// Asking goes through an Asker, one question at a time even when the agent
// runs tool calls in parallel. An answer of AllowAlways allows the same subject
// for the rest of the Policy's life. A denied call returns a *DeniedError,
// which the agent reports to the model as a failed tool call.
//
//	policy, err := permission.New(
//		permission.WithAllow("read", "grep", "glob", "bash(go test *)", "bash(go build *)"),
//		permission.WithDeny("bash(rm -rf *)", "bash(git push *)"),
//		permission.WithAsker(askInTerminal),
//	)
//	policy.Apply(readTool.Tool(), bashTool.Tool(), editTool.Tool())
package permission
