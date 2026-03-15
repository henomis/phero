package bash

import (
	"context"
	"os/exec"
	"strings"
	"testing"
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
	}
	if !strings.Contains(out.Output, "exit code: 7") {
		t.Fatalf("expected exit code marker, got: %q", out.Output)
	}
}
