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

func TestBashTool_InheritsEnvByDefault(t *testing.T) {
	requireBash(t)
	t.Setenv("PHERO_TEST_SECRET", "s3cret")

	tool, err := New()
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo $PHERO_TEST_SECRET"})
	if err != nil || strings.TrimSpace(out.Output) != "s3cret" {
		t.Fatalf("output = %+v, %v; want the inherited variable", out, err)
	}
}

func TestBashTool_EnvAllowlistAndExtraEnv(t *testing.T) {
	requireBash(t)
	t.Setenv("PHERO_TEST_SECRET", "s3cret")

	tool, err := New(WithEnvAllowlist(DefaultEnvAllowlist...), WithEnv("PHERO_TEST_EXTRA=extra"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{Command: `echo "[$PHERO_TEST_SECRET][$PHERO_TEST_EXTRA]"; command -v ls`})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(out.Output, "[][extra]\n") {
		t.Fatalf("output = %q, want the secret filtered out and the extra entry set", out.Output)
	}

	// PATH is in the allowlist, so commands are still found.
	if !strings.Contains(out.Output, "/ls") {
		t.Fatalf("output = %q, want ls found on PATH", out.Output)
	}
}

// echoRunner runs every command as `echo <prefix> <command>`.
type echoRunner struct{ prefix string }

func (r echoRunner) Command(ctx context.Context, command string, opts RunOptions) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, "echo", r.prefix, command)
	cmd.Dir = opts.Dir

	return cmd, nil
}

func TestBashTool_WithRunner(t *testing.T) {
	tool, err := New(WithRunner(echoRunner{prefix: "sandboxed:"}))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "make test"})
	if err != nil || strings.TrimSpace(out.Output) != "sandboxed: make test" {
		t.Fatalf("output = %+v, %v; want the runner's command", out, err)
	}
}
