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

package mcp

import (
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// baseEnvVars are the only parent-process variables forwarded to an MCP stdio
// subprocess by [StdioTransport]. They are what a command needs to be found and
// to run correctly; none of them is a credential.
var baseEnvVars = []string{"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL"}

// StdioTransport builds a transport for an MCP server run as a subprocess, with
// a deliberately minimal environment.
//
// The subprocess does *not* inherit the parent's environment. That is the whole
// point of this function: the parent of an agent process typically holds
// provider API keys, database DSNs and broker credentials, and an MCP server is
// third-party code chosen by configuration. Passing the environment through —
// which is what exec.Command does by default, and therefore what an embedder
// gets by writing the obvious thing — hands every one of those secrets to it.
//
// Only PATH, HOME, TMPDIR, LANG and LC_ALL are forwarded, and only when they are
// actually set. Anything else the server needs must be named explicitly in env,
// which also makes it visible in the configuration that granted it.
func StdioTransport(command string, args []string, env map[string]string) *mcp.CommandTransport {
	cmd := exec.Command(command, args...) //nolint:gosec // the command is the caller's to choose

	cmd.Env = baseEnv()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	return &mcp.CommandTransport{Command: cmd}
}

// baseEnv returns the minimal environment, copying only the whitelisted
// variables that are set in this process.
func baseEnv() []string {
	env := make([]string, 0, len(baseEnvVars))

	for _, k := range baseEnvVars {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}

	return env
}

// AllowDeny builds a [ToolFilter] from an allow list and a deny list.
//
// deny wins over allow, so a name in both is excluded — the safe reading when
// two rules disagree. An empty allow list means "everything not denied"; a
// non-empty one means "only these". Both empty yields a nil filter, which
// [Server.AsTools] treats as no filtering at all.
func AllowDeny(allow, deny []string) ToolFilter {
	if len(allow) == 0 && len(deny) == 0 {
		return nil
	}

	allowed := make(map[string]bool, len(allow))
	for _, name := range allow {
		allowed[name] = true
	}

	denied := make(map[string]bool, len(deny))
	for _, name := range deny {
		denied[name] = true
	}

	return func(name string) bool {
		if denied[name] {
			return false
		}

		if len(allowed) > 0 {
			return allowed[name]
		}

		return true
	}
}
