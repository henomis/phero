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
	"slices"
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

func TestBubblewrap_ExtraEnvKeepsMinimalEnvironment(t *testing.T) {
	t.Setenv("PHERO_TEST_SECRET", "s3cret")
	tool := newSandboxedTool(t, t.TempDir())
	WithEnv("PHERO_TEST_EXTRA=extra")(tool)

	out, err := tool.run(context.Background(), &Input{Command: `echo "[$PHERO_TEST_SECRET][$PHERO_TEST_EXTRA]"`})
	if err != nil || strings.TrimSpace(out.Output) != "[][extra]" {
		t.Fatalf("output = %+v, %v; want the secret left out and the extra entry set", out, err)
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

func TestBubblewrap_NoHostSockets(t *testing.T) {
	tool := newSandboxedTool(t, t.TempDir())

	// connect() ignores read-only mounts: a host socket such as docker.sock or
	// the D-Bus session bus would let a command act outside the sandbox.
	out, err := tool.run(context.Background(), &Input{
		Command: `find / \( -path /proc -o -path /sys \) -prune -o -type s -print 2>/dev/null; true`,
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(out.Output); got != "" {
		t.Fatalf("sockets visible in the sandbox:\n%s", got)
	}
}

func TestBubblewrap_HostPathsHiddenUnlessReadOnlyPath(t *testing.T) {
	// This package's directory is a host path outside the system directories
	// and outside the workspace.
	pkgDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}

	for _, sys := range SystemPaths() {
		if strings.HasPrefix(pkgDir, sys+"/") {
			t.Skipf("the package lives under %s, which the sandbox mounts", sys)
		}
	}

	source := filepath.Join(pkgDir, "runner_bwrap.go")
	command := "cat " + source + " >/dev/null 2>&1 && echo read; touch " + filepath.Join(pkgDir, "x") + " 2>/dev/null || echo ro"

	hidden := newSandboxedTool(t, t.TempDir())
	if got := runOK(t, hidden, command); got != "ro" {
		t.Fatalf("got %q, want the host path hidden", got)
	}

	shown := newSandboxedTool(t, t.TempDir(), WithReadOnlyPaths(pkgDir))
	if got := runOK(t, shown, command); got != "read\nro" {
		t.Fatalf("got %q, want the read-only path readable and not writable", got)
	}
}

func TestBubblewrap_HomeIsPrivateAndWritable(t *testing.T) {
	tool := newSandboxedTool(t, t.TempDir())

	// Tools write caches under $HOME: it is an empty tmpfs, not the host's.
	if got := runOK(t, tool, `mkdir -p "$HOME/.cache/x" && ls -A "$HOME"`); got != ".cache" {
		t.Fatalf("got %q, want an empty, writable home", got)
	}
}

func TestBubblewrap_InnerMountWins(t *testing.T) {
	workspace := t.TempDir()
	for _, dir := range []string{".git", "ro/cache"} {
		if err := os.MkdirAll(filepath.Join(workspace, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}

	// A read-only path inside the workspace, and a writable path inside it.
	tool := newSandboxedTool(t, workspace,
		WithReadOnlyPaths(filepath.Join(workspace, ".git"), filepath.Join(workspace, "ro")),
		WithWritablePaths(filepath.Join(workspace, "ro", "cache")),
	)

	got := runOK(t, tool, "(echo x > .git/config) 2>/dev/null || echo git-ro; (echo x > ro/f) 2>/dev/null || echo ro-ro; echo x > ro/cache/f && echo cache-rw")
	if got != "git-ro\nro-ro\ncache-rw" {
		t.Fatalf("got %q, want the innermost mount to decide", got)
	}
}

func TestNewBubblewrapRunner_RefusesBroadPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	project := filepath.Join(home, "project")

	cases := []struct {
		name      string
		workspace string
		opts      []BubblewrapOption
		wantErr   bool
	}{
		{"home", home, nil, true},
		{"home parent", filepath.Dir(home), nil, true},
		{"root", "/", nil, true},
		{"read-only home", project, []BubblewrapOption{WithReadOnlyPaths(home)}, true},
		{"writable root", project, []BubblewrapOption{WithWritablePaths("/")}, true},
		{"project under home", project, []BubblewrapOption{WithReadOnlyPaths(filepath.Join(home, "go"))}, false},
	}

	for _, tc := range cases {
		_, err := NewBubblewrapRunner(tc.workspace, tc.opts...)
		if errors.Is(err, ErrBubblewrapNotFound) {
			t.Skip("bwrap not installed")
		}

		if got := errors.Is(err, ErrPathTooBroad); got != tc.wantErr {
			t.Errorf("%s: err = %v, want ErrPathTooBroad: %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestBubblewrap_NewSession(t *testing.T) {
	runner, err := NewBubblewrapRunner(t.TempDir())
	if errors.Is(err, ErrBubblewrapNotFound) {
		t.Skip("bwrap not installed")
	}

	if err != nil {
		t.Fatal(err)
	}

	cmd, err := runner.Command(context.Background(), "true", RunOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Without a new session the command shares the caller's terminal, and
	// TIOCSTI could push input into it.
	if !slices.Contains(cmd.Args, "--new-session") {
		t.Fatalf("args = %q, want --new-session", cmd.Args)
	}
}

func TestNewBubblewrapRunner_RefusesBroadPathsThroughSymlinks(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")

	if err := os.Mkdir(home, 0o750); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)

	for name, target := range map[string]string{"home": home, "root": "/"} {
		link := filepath.Join(root, "ln-"+name)
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}

		_, err := NewBubblewrapRunner(link)
		if errors.Is(err, ErrBubblewrapNotFound) {
			t.Skip("bwrap not installed")
		}

		if !errors.Is(err, ErrPathTooBroad) {
			t.Errorf("workspace %s -> %s: err = %v, want ErrPathTooBroad", link, target, err)
		}
	}

	// A symlinked $HOME, with the workspace given as its real path.
	realHome := filepath.Join(root, "data")
	if err := os.Mkdir(realHome, 0o750); err != nil {
		t.Fatal(err)
	}

	linkedHome := filepath.Join(root, "linked-home")
	if err := os.Symlink(realHome, linkedHome); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", linkedHome)

	if _, err := NewBubblewrapRunner(realHome); !errors.Is(err, ErrPathTooBroad) {
		t.Errorf("real path of a symlinked home: err = %v, want ErrPathTooBroad", err)
	}
}
