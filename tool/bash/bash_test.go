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
