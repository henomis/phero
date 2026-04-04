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
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/henomis/phero/llm"
)

// Input represents the input for the bash_tool.
//
// Command is the bash command to execute. Description provides context on why the command is being run.
type Input struct {
	Command     string `json:"command" jsonschema:"description=bash command to run"`
	Description string `json:"description" jsonschema:"description=why you're running it"`
}

// Output represents the output from bash_tool.
//
// It returns stdout + stderr combined as a plain text string.
type Output struct {
	Output string `json:"output"`
}

// SafeModeTimeout is the default command execution timeout applied by WithSafeMode.
const SafeModeTimeout = 30 * time.Second

// safeModeBlocklist contains substring patterns that are blocked when safe mode is enabled.
var safeModeBlocklist = []string{
	"rm -rf /",
	"rm -fr /",
	"dd if=",
	"mkfs",
	":(){ :|:",
	"> /dev/sd",
	"> /dev/hd",
	"> /dev/nvme",
	"chmod 777 /",
	"| bash",
	"| sh",
}

// Tool is a tool that runs bash commands.
type Tool struct {
	tool       *llm.Tool
	workingDir string
	timeout    time.Duration
	blocklist  []string
	allowlist  []string
}

// Option represents a configuration option for the bash_tool.
type Option func(*Tool)

// New creates a new instance of the bash_tool.
func New(options ...Option) (*Tool, error) {
	name := "bash"
	description := "Use this tool to run bash commands"

	bashTool := &Tool{}

	for _, option := range options {
		option(bashTool)
	}

	tool, err := llm.NewTool(
		name,
		description,
		bashTool.run,
	)
	if err != nil {
		return nil, err
	}

	bashTool.tool = tool
	return bashTool, nil
}

// Tool returns the llm.Tool representation.
func (t *Tool) Tool() *llm.Tool {
	return t.tool
}

// WithWorkingDirectory sets the directory in which bash commands are executed.
func WithWorkingDirectory(dir string) Option {
	return func(t *Tool) {
		t.workingDir = dir
	}
}

// WithTimeout sets a maximum execution duration for each command.
// When the context deadline is shorter than the configured timeout, the
// context deadline takes precedence. A value of 0 (the default) means no
// additional timeout beyond the context.
func WithTimeout(d time.Duration) Option {
	return func(t *Tool) {
		t.timeout = d
	}
}

// WithBlocklist adds patterns to a blocklist checked case-insensitively
// against the raw command string before execution. If any pattern matches,
// run returns ErrCommandBlocked.
func WithBlocklist(patterns ...string) Option {
	return func(t *Tool) {
		for _, p := range patterns {
			t.blocklist = append(t.blocklist, strings.ToLower(p))
		}
	}
}

// WithAllowlist sets patterns that the command must match at least one of
// (case-insensitive substring match) to be allowed to run. If the allowlist
// is non-empty and the command does not match any pattern, run returns
// ErrCommandNotAllowed.
func WithAllowlist(patterns ...string) Option {
	return func(t *Tool) {
		for _, p := range patterns {
			t.allowlist = append(t.allowlist, strings.ToLower(p))
		}
	}
}

// WithSafeMode enables a safe execution profile suitable for local assistants
// and examples. It applies a default blocklist of dangerous command patterns
// and sets a 30-second execution timeout.
func WithSafeMode() Option {
	return func(t *Tool) {
		t.blocklist = append(t.blocklist, safeModeBlocklist...)
		if t.timeout == 0 {
			t.timeout = SafeModeTimeout
		}
	}
}

func (t *Tool) run(ctx context.Context, input *Input) (*Output, error) {
	if input == nil {
		return nil, ErrNilInput
	}
	if strings.TrimSpace(input.Command) == "" {
		return nil, ErrCommandRequired
	}

	lower := strings.ToLower(input.Command)
	for _, pat := range t.blocklist {
		if strings.Contains(lower, pat) {
			return nil, ErrCommandBlocked
		}
	}
	if len(t.allowlist) > 0 {
		allowed := false
		for _, pat := range t.allowlist {
			if strings.Contains(lower, pat) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, ErrCommandNotAllowed
		}
	}

	runCtx := ctx
	if t.timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, t.timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, "bash", "-c", input.Command)
	if t.workingDir != "" {
		cmd.Dir = t.workingDir
	}

	combined, err := cmd.CombinedOutput()
	out := string(combined)

	if err == nil {
		return &Output{Output: out}, nil
	}

	// If the context expired (timeout or cancellation), surface that error
	// rather than the process exit error so callers can distinguish policy failures.
	if runCtx.Err() != nil {
		return nil, runCtx.Err()
	}

	// If the command executed but failed (non-zero exit), return output with an inline marker
	// so callers can infer failure without an explicit exit code field.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += fmt.Sprintf("exit code: %d", exitErr.ExitCode())
		return &Output{Output: out}, nil
	}

	// For execution failures (e.g., bash missing), surface the error.
	return nil, err
}
