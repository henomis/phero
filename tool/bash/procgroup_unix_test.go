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

//go:build unix

package bash

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// childPID reads the PID the test command echoes on its first line.
func childPID(t *testing.T, output string) int {
	t.Helper()

	pid, err := strconv.Atoi(strings.TrimSpace(strings.SplitN(output, "\n", 2)[0]))
	if err != nil {
		t.Fatalf("no child PID in output %q", output)
	}

	return pid
}

// waitGone fails unless process pid has exited within a few seconds.
func waitGone(t *testing.T, pid int) {
	t.Helper()

	for range 50 {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("child process %d survived", pid)
}

func TestBashTool_TimeoutKillsChildren(t *testing.T) {
	requireBash(t)

	tool, err := New(WithTimeout(200 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "sleep 30 & echo $!; wait"})
	if err != nil || !out.TimedOut {
		t.Fatalf("run = %+v, %v; want a timeout", out, err)
	}

	waitGone(t, childPID(t, out.Output))
}

func TestBashTool_KillShellKillsChildren(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatal(err)
	}

	started, err := tool.run(context.Background(), &Input{Command: "sleep 30 & echo $!; wait", RunInBackground: true})
	if err != nil {
		t.Fatal(err)
	}

	var first string

	for range 50 {
		got, outErr := tool.output(context.Background(), &OutputInput{BashID: started.BashID})
		if outErr != nil {
			t.Fatal(outErr)
		}

		if first += got.Output; strings.Contains(first, "\n") {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	pid := childPID(t, first)

	if killed, killErr := tool.kill(context.Background(), &KillShellInput{ShellID: started.BashID}); killErr != nil || !killed.Killed {
		t.Fatalf("kill = %+v, %v", killed, killErr)
	}

	waitGone(t, pid)
}

func TestBashTool_FinishedShellIsForgottenAfterRead(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatal(err)
	}

	started, err := tool.run(context.Background(), &Input{Command: "echo hi", RunInBackground: true})
	if err != nil {
		t.Fatal(err)
	}

	for range 50 {
		got, outErr := tool.output(context.Background(), &OutputInput{BashID: started.BashID})
		if outErr != nil {
			t.Fatal(outErr)
		}

		if !got.Running {
			break
		}

		time.Sleep(20 * time.Millisecond)
	}

	if _, err = tool.output(context.Background(), &OutputInput{BashID: started.BashID}); !errors.Is(err, ErrShellNotFound) {
		t.Fatalf("err = %v, want ErrShellNotFound once the finished output was read", err)
	}
}
