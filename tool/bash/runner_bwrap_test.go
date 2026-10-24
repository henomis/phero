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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newSandboxedTool(t *testing.T, workspace string, opts ...BubblewrapOption) *Tool {
	t.Helper()
	requireBash(t)

	runner, err := NewBubblewrapRunner(workspace, opts...)
	if errors.Is(err, ErrBubblewrapNotFound) {
		t.Skip("bwrap not installed")
	}

	if err != nil {
		t.Fatal(err)
	}

	tool, err := New(WithRunner(runner), WithWorkingDirectory(workspace))
	if err != nil {
		t.Fatal(err)
	}

	// Some systems forbid the unprivileged user namespaces bwrap needs.
	if out, runErr := tool.run(context.Background(), &Input{Command: "true"}); runErr != nil || strings.Contains(out.Output, "exit code") {
		t.Skipf("bwrap cannot run here: %+v %v", out, runErr)
	}

	return tool
}

func TestBubblewrap_WritesOnlyInWorkspace(t *testing.T) {
	workspace, outside := t.TempDir(), t.TempDir()
	tool := newSandboxedTool(t, workspace)

	out, err := tool.run(context.Background(), &Input{Command: "echo ok > inside.txt && echo nope > " + filepath.Join(outside, "x.txt")})
	if err != nil {
		t.Fatal(err)
	}

	if data, readErr := os.ReadFile(filepath.Join(workspace, "inside.txt")); readErr != nil || string(data) != "ok\n" {
		t.Fatalf("workspace write failed: %q %v (output %q)", data, readErr, out.Output)
	}

	if _, statErr := os.Stat(filepath.Join(outside, "x.txt")); statErr == nil {
		t.Fatal("the sandbox wrote outside the workspace")
	}

	if !strings.Contains(out.Output, "exit code") {
		t.Fatalf("output = %q, want the outside write to fail", out.Output)
	}
}

func TestBubblewrap_NoNetworkByDefault(t *testing.T) {
	workspace := t.TempDir()

	// /proc/net/dev lists the interfaces of the command's network namespace.
	interfaces := func(tool *Tool) string {
		out, err := tool.run(context.Background(), &Input{Command: "grep ':' /proc/net/dev | cut -d: -f1 | tr -d ' ' | sort | tr '\\n' ' '"})
		if err != nil {
			t.Fatal(err)
		}

		return strings.TrimSpace(out.Output)
	}

	if got := interfaces(newSandboxedTool(t, workspace)); got != "lo" {
		t.Fatalf("interfaces = %q, want only lo", got)
	}

	if got := interfaces(newSandboxedTool(t, workspace, WithNetwork(true))); got == "lo" {
		t.Skip("the host has no interface but lo: WithNetwork cannot be told apart")
	}
}

func TestBubblewrap_MinimalEnvironment(t *testing.T) {
	t.Setenv("PHERO_TEST_SECRET", "s3cret")
	tool := newSandboxedTool(t, t.TempDir())

	out, err := tool.run(context.Background(), &Input{Command: `echo "[$PHERO_TEST_SECRET]"`})
	if err != nil || strings.TrimSpace(out.Output) != "[]" {
		t.Fatalf("output = %+v, %v; want the secret left out", out, err)
	}
}

func TestBubblewrap_TimeoutKillsSandbox(t *testing.T) {
	workspace := t.TempDir()
	tool := newSandboxedTool(t, workspace)
	tool.timeout = 300 * time.Millisecond

	// A background loop keeps rewriting a heartbeat file in the workspace.
	out, err := tool.run(context.Background(), &Input{
		Command: "(while true; do date +%s%N > tick; sleep 0.05; done) & wait",
	})
	if err != nil || !out.TimedOut {
		t.Fatalf("run = %+v, %v; want a timeout", out, err)
	}

	tick := filepath.Join(workspace, "tick")

	before, err := os.ReadFile(tick)
	if err != nil {
		t.Fatalf("the loop never ran: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// The sandbox's PID namespace dies with bwrap: the loop must be gone.
	if after, _ := os.ReadFile(tick); string(after) != string(before) {
		t.Fatal("a sandboxed process survived the timeout")
	}
}

func TestBubblewrap_PersistentShell(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Mkdir(filepath.Join(workspace, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}

	tool := newSandboxedTool(t, workspace)
	tool.persistent = true

	runOK(t, tool, "cd sub && export PHERO_TEST_VAR=1")

	if got := runOK(t, tool, `echo "$PWD $PHERO_TEST_VAR"`); got != filepath.Join(workspace, "sub")+" 1" {
		t.Fatalf("got %q, want the state carried inside the sandbox", got)
	}
}
