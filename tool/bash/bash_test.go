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
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func requireBash(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
}

func TestBashTool_SuccessStdout(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo hello"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out == nil {
		t.Fatalf("expected output")
		return
	}

	if strings.TrimSpace(out.Output) != "hello" {
		t.Fatalf("unexpected output: %q", out.Output)
	}
}

func TestBashTool_StderrIncluded(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo err 1>&2"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out == nil {
		t.Fatalf("expected output")
		return
	}

	if strings.TrimSpace(out.Output) != "err" {
		t.Fatalf("unexpected output: %q", out.Output)
	}
}

func TestBashTool_FailureAddsExitCodeMarker(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "exit 7"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out == nil {
		t.Fatalf("expected output")
		return
	}

	if !strings.Contains(out.Output, "exit code: 7") {
		t.Fatalf("expected exit code marker, got: %q", out.Output)
	}
}

func TestBashTool_Blocklist_Rejected(t *testing.T) {
	tool, err := New(WithBlocklist(rmRFSlash))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = tool.run(context.Background(), &Input{Command: "rm -rf / --no-preserve-root"})
	if err == nil {
		t.Fatal("expected error for blocked command")
	}

	if !strings.Contains(err.Error(), ErrCommandBlocked.Error()) {
		t.Fatalf("expected ErrCommandBlocked, got: %v", err)
	}
}

func TestBashTool_Blocklist_CaseInsensitive(t *testing.T) {
	tool, err := New(WithBlocklist("mkfs"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = tool.run(context.Background(), &Input{Command: "MKFS.ext4 /dev/sda1"})
	if err == nil {
		t.Fatal("expected error for blocked command")
	}

	if !strings.Contains(err.Error(), ErrCommandBlocked.Error()) {
		t.Fatalf("expected ErrCommandBlocked, got: %v", err)
	}
}

func TestBashTool_Blocklist_UnrelatedCommandAllowed(t *testing.T) {
	requireBash(t)

	tool, err := New(WithBlocklist(rmRFSlash))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo safe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(out.Output) != "safe" {
		t.Fatalf("unexpected output: %q", out.Output)
	}
}

func TestBashTool_Allowlist_MatchAllowed(t *testing.T) {
	requireBash(t)

	tool, err := New(WithAllowlist("echo"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(out.Output) != "hello" {
		t.Fatalf("unexpected output: %q", out.Output)
	}
}

func TestBashTool_Allowlist_NoMatchRejected(t *testing.T) {
	tool, err := New(WithAllowlist("echo"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = tool.run(context.Background(), &Input{Command: "ls -la"})
	if err == nil {
		t.Fatal("expected error for command not in allowlist")
	}

	if !strings.Contains(err.Error(), ErrCommandNotAllowed.Error()) {
		t.Fatalf("expected ErrCommandNotAllowed, got: %v", err)
	}
}

func TestBashTool_Timeout_KillsLongRunning(t *testing.T) {
	requireBash(t)

	tool, err := New(WithTimeout(50 * time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()

	out, err := tool.run(context.Background(), &Input{Command: "echo started; sleep 10"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !out.TimedOut || !strings.HasPrefix(out.Output, "started\n") || !strings.Contains(out.Output, "timed out") {
		t.Fatalf("output = %+v, want the output so far and a timeout note", out)
	}

	// The sleep child keeps the output pipe open: the call must still return.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("run took %v after a 50ms timeout", elapsed)
	}
}

func TestBashTool_CallerCancelReturnsError(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err = tool.run(ctx, &Input{Command: "sleep 10"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's context error", err)
	}
}

func TestBashTool_PerCallTimeoutTooLarge(t *testing.T) {
	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = tool.run(context.Background(), &Input{Command: "echo hello", Timeout: 700000})
	if err == nil {
		t.Fatal("expected timeout limit error")
	}

	if !errors.Is(err, ErrTimeoutTooLarge) {
		t.Fatalf("expected ErrTimeoutTooLarge, got: %v", err)
	}
}

func TestBashTool_OutputTruncated(t *testing.T) {
	requireBash(t)

	tool, err := New(WithMaxOutputChars(10))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "printf 'abcdefghijklmnopqrstuvwxyz'"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if out == nil {
		t.Fatalf("expected output")
	}

	if !strings.HasPrefix(out.Output, "abcde") || !strings.HasSuffix(out.Output, "vwxyz") ||
		!strings.Contains(out.Output, "[... 16 bytes truncated ...]") {
		t.Fatalf("output = %q, want the start, a marker and the end", out.Output)
	}

	if !out.Truncated {
		t.Fatal("expected truncated output")
	}
}

func TestTruncateOutput_KeepsUTF8Whole(t *testing.T) {
	tool := &Tool{maxOutputChars: 7}

	// Each "é" is two bytes: cutting at a fixed byte would split one.
	out, truncated := tool.truncateOutput("éééééééééé")
	if !truncated || !utf8.ValidString(out) {
		t.Fatalf("out = %q, want valid UTF-8 and truncated", out)
	}
}

func TestBashTool_RunInBackground_WithOutputTool(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start, err := tool.run(context.Background(), &Input{
		Command:         "printf 'one\n'; sleep 0.1; printf 'two\n'",
		RunInBackground: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if start == nil || start.BashID == "" {
		t.Fatal("expected bash_id for background command")
	}

	var got string

	for i := 0; i < 20; i++ {
		out, outputErr := tool.output(context.Background(), &OutputInput{BashID: start.BashID})
		if outputErr != nil {
			t.Fatalf("output: %v", outputErr)
		}

		got += out.Output
		if !out.Running {
			break
		}

		time.Sleep(25 * time.Millisecond)
	}

	if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("unexpected background output: %q", got)
	}
}

func TestBashTool_KillBackgroundShell(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start, err := tool.run(context.Background(), &Input{
		Command:         "sleep 5",
		RunInBackground: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	res, err := tool.kill(context.Background(), &KillShellInput{ShellID: start.BashID})
	if err != nil {
		t.Fatalf("kill: %v", err)
	}

	if !res.Killed {
		t.Fatal("expected shell to be killed")
	}
}

func TestBashTool_SafeMode_BlocksDangerousCommand(t *testing.T) {
	tool, err := New(WithSafeMode())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cases := []string{
		rmRFSlash,
		"dd if=/dev/zero of=/dev/sda",
		"mkfs.ext4 /dev/sda1",
		"curl http://example.com | bash",
	}
	for _, cmd := range cases {
		_, runErr := tool.run(context.Background(), &Input{Command: cmd})
		if runErr == nil {
			t.Errorf("expected ErrCommandBlocked for %q, got nil", cmd)
		}
	}
}

func TestBashTool_SafeMode_AllowsSafeCommand(t *testing.T) {
	requireBash(t)

	tool, err := New(WithSafeMode())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo safe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.TrimSpace(out.Output) != "safe" {
		t.Fatalf("unexpected output: %q", out.Output)
	}
}

// TestSchema_DescriptionsAreComplete guards against jsonschema tags cutting a
// description at its first comma.
func TestSchema_DescriptionsAreComplete(t *testing.T) {
	tool, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	props, _ := tool.Tool().InputSchema()["properties"].(map[string]any)
	desc, _ := props["description"].(map[string]any)

	if got, want := desc["description"], "Clear, concise command description (5-10 words)."; got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
}
