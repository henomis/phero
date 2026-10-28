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

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// Runner builds the process that runs one shell command. It decides how the
// command is launched (directly, in a sandbox, in a container); the tool keeps
// the rest: timeouts, process groups, background shells and output handling.
//
// Command must create the process with exec.CommandContext(ctx, ...), so that
// cancelling ctx kills it, and must not start it.
type Runner interface {
	Command(ctx context.Context, command string, opts RunOptions) (*exec.Cmd, error)
}

// RunOptions holds what the tool asks of a Runner for one command.
type RunOptions struct {
	// Dir is the working directory; empty means the current directory.
	Dir string
	// Env is the command's environment as KEY=VALUE entries; nil means the
	// runner's default: the agent process's environment for LocalRunner,
	// DefaultEnvAllowlist() for BubblewrapRunner.
	Env []string
	// ExtraEnv holds KEY=VALUE entries (WithEnv) to add after Env, or after
	// the runner's default when Env is nil. A Runner must apply them.
	ExtraEnv []string
}

// LocalRunner runs commands with bash on the host, with no isolation. It is
// the default Runner.
type LocalRunner struct{}

// Command returns `bash -c command` in opts.Dir with opts.Env and
// opts.ExtraEnv.
func (LocalRunner) Command(ctx context.Context, command string, opts RunOptions) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command) //nolint:gosec // the tool runs arbitrary commands by design
	cmd.Dir = opts.Dir

	cmd.Env = opts.Env
	if len(opts.ExtraEnv) > 0 {
		if cmd.Env == nil {
			cmd.Env = os.Environ()
		}

		cmd.Env = append(slices.Clip(cmd.Env), opts.ExtraEnv...)
	}

	return cmd, nil
}

// DefaultEnvAllowlist returns the environment a sandboxed command gets, and a
// sane choice for WithEnvAllowlist: what common tools need, and no secrets.
// Each call returns a new slice, so a caller cannot widen the default for
// others.
func DefaultEnvAllowlist() []string {
	return []string{
		"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "TMPDIR", "TZ",
	}
}

// filterEnv returns the entries of environ whose name is in names.
func filterEnv(environ, names []string) []string {
	out := make([]string, 0, len(names))

	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(names, name) {
			out = append(out, kv)
		}
	}

	return out
}

// commandEnv returns the base environment of a command: the agent's process
// environment filtered by the allowlist, or nil, the runner's default, when
// no allowlist is set. The WithEnv entries go to the runner separately
// (RunOptions.ExtraEnv), so they never turn a sandbox's minimal default into
// the whole environment.
func (t *Tool) commandEnv() []string {
	if t.envAllowlist == nil {
		return nil
	}

	return filterEnv(os.Environ(), t.envAllowlist)
}

// newCommand builds the process for command with the tool's runner, and sets
// what every command gets: the output wait delay and its own process group.
func (t *Tool) newCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	opts := RunOptions{Dir: t.workingDir, Env: t.commandEnv(), ExtraEnv: t.extraEnv}
	if t.persistent {
		opts.Dir = t.state.dir(t.workingDir)
	}

	cmd, err := t.runner.Command(ctx, command, opts)
	if err != nil {
		return nil, err
	}

	if t.persistent {
		cmd.Env = t.state.apply(startEnv(cmd))
	}

	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)

	return cmd, nil
}

// startEnv returns the environment cmd starts with.
func startEnv(cmd *exec.Cmd) []string {
	if cmd.Env != nil {
		return cmd.Env
	}

	return os.Environ()
}

// recordState updates the persistent shell state from the report of cmd,
// which has exited; completed is false when it was killed.
func (t *Tool) recordState(cmd *exec.Cmd, report *stateReport, completed bool) {
	cwd, env, ok := report.read()
	if ok && completed {
		t.state.update(cwd, startEnv(cmd), env)
	}
}
