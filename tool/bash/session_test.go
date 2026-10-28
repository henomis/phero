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
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runOK(t *testing.T, tool *Tool, command string) string {
	t.Helper()

	out, err := tool.run(context.Background(), &Input{Command: command})
	if err != nil {
		t.Fatalf("run %q: %v", command, err)
	}

	return strings.TrimSpace(out.Output)
}

func TestPersistentShell_CarriesDirAndEnv(t *testing.T) {
	requireBash(t)

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PHERO_TEST_DROP", "x")

	tool, err := New(WithPersistentShell(), WithWorkingDirectory(root))
	if err != nil {
		t.Fatal(err)
	}

	runOK(t, tool, "cd sub && export PHERO_TEST_VAR='a b=c' && unset PHERO_TEST_DROP")

	if got := runOK(t, tool, "pwd"); got != filepath.Join(root, "sub") {
		t.Fatalf("pwd = %q, want the directory of the previous cd", got)
	}

	if got := runOK(t, tool, `echo "[$PHERO_TEST_VAR][$PHERO_TEST_DROP]"`); got != "[a b=c][]" {
		t.Fatalf("env = %q, want the exported variable set and the unset one gone", got)
	}
}

func TestPersistentShell_KeepsExitCodeAndOutput(t *testing.T) {
	requireBash(t)

	tool, err := New(WithPersistentShell())
	if err != nil {
		t.Fatal(err)
	}

	if got := runOK(t, tool, "echo out; false"); got != "out\nexit code: 1" {
		t.Fatalf("output = %q, want the command's output and exit code only", got)
	}
}

func TestPersistentShell_BackgroundChildDoesNotBlock(t *testing.T) {
	requireBash(t)

	tool, err := New(WithPersistentShell(), WithWorkingDirectory(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	// The sleep keeps stdout open until WaitDelay, but not the state report.
	runOK(t, tool, "cd / && (sleep 3 >/dev/null 2>&1 &)")

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("run took %v: a background child held the state report open", elapsed)
	}

	if got := runOK(t, tool, "pwd"); got != "/" {
		t.Fatalf("pwd = %q, want /", got)
	}
}

func TestPersistentShell_TimeoutKeepsState(t *testing.T) {
	requireBash(t)

	root := t.TempDir()

	tool, err := New(WithPersistentShell(), WithWorkingDirectory(root), WithTimeout(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	if out, runErr := tool.run(context.Background(), &Input{Command: "cd / && sleep 5"}); runErr != nil || !out.TimedOut {
		t.Fatalf("run = %+v, %v; want a timeout", out, runErr)
	}

	if got := runOK(t, tool, "pwd"); got != root {
		t.Fatalf("pwd = %q, want %q: a killed command changes nothing", got, root)
	}
}

func TestStatelessShell_DoesNotCarryDir(t *testing.T) {
	requireBash(t)

	root := t.TempDir()

	tool, err := New(WithWorkingDirectory(root))
	if err != nil {
		t.Fatal(err)
	}

	runOK(t, tool, "cd /")

	if got := runOK(t, tool, "pwd"); got != root {
		t.Fatalf("pwd = %q, want %q", got, root)
	}

	if strings.Contains(tool.Tool().Description(), "persistent") {
		t.Fatal("a stateless tool must not claim a persistent session")
	}
}
