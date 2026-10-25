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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/henomis/phero/v2/llm"
)

// Input represents the input for the bash_tool.
//
// Command is the bash command to execute. Description provides context on why the command is being run.
type Input struct {
	Command         string `json:"command" jsonschema:"description=The command to execute."`
	Description     string `json:"description,omitempty" jsonschema_description:"Clear, concise command description (5-10 words)."` //nolint:lll
	Timeout         int    `json:"timeout,omitempty" jsonschema:"description=Optional timeout in milliseconds (max 600000)."`       //nolint:lll
	RunInBackground bool   `json:"run_in_background,omitempty" jsonschema:"description=Set true to run the command in background."` //nolint:lll
}

// Output represents the output from bash_tool.
//
// It returns stdout + stderr combined as a plain text string.
type Output struct {
	Output    string `json:"output"`
	BashID    string `json:"bash_id,omitempty"`
	Running   bool   `json:"running"`
	Truncated bool   `json:"truncated"`
	// TimedOut reports that the command was killed when its timeout expired;
	// Output holds what it printed until then.
	TimedOut bool `json:"timed_out,omitempty"`
}

// OutputInput represents input for retrieving background shell output.
type OutputInput struct {
	BashID string `json:"bash_id" jsonschema:"description=ID of the background shell to read."`
	Filter string `json:"filter,omitempty" jsonschema:"description=Optional regular expression to include only matching lines."` //nolint:lll
}

// OutputOutput represents incremental output from a background shell.
type OutputOutput struct {
	Output    string `json:"output"`
	Running   bool   `json:"running"`
	Truncated bool   `json:"truncated"`
}

// KillShellInput represents input for terminating a background shell.
type KillShellInput struct {
	ShellID string `json:"shell_id" jsonschema:"description=ID of the background shell to terminate."`
}

// KillShellOutput represents the result of terminating a background shell.
type KillShellOutput struct {
	Killed bool `json:"killed"`
}

// SafeModeTimeout is the default command execution timeout applied by WithSafeMode.
const SafeModeTimeout = 30 * time.Second

// DefaultTimeout is the default command execution timeout used when no timeout
// is provided.
const DefaultTimeout = 120 * time.Second

// MaxTimeout is the maximum allowed timeout for command execution.
const MaxTimeout = 600 * time.Second

// MaxOutputChars is the maximum number of output bytes returned from a single
// tool call before truncation. Truncated output keeps its start and its end,
// where errors usually are, and marks the cut.
const MaxOutputChars = 30000

// waitDelay is how long a command killed at its timeout may keep its output
// open (a child process still writing) before the pipes are closed, so the
// call returns.
const waitDelay = 2 * time.Second

const randomIDBytes = 8

const rmRFSlash = "rm -rf /"

// safeModeBlocklist contains substring patterns that are blocked when safe mode is enabled.
var safeModeBlocklist = []string{
	rmRFSlash,
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
	tool           *llm.Tool
	outputTool     *llm.Tool
	killTool       *llm.Tool
	workingDir     string
	timeout        time.Duration
	defaultTimeout time.Duration
	maxTimeout     time.Duration
	maxOutputChars int
	blocklist      []string
	allowlist      []string
	runner         Runner
	envAllowlist   []string
	extraEnv       []string
	persistent     bool
	state          shellState

	mu    sync.RWMutex
	shell map[string]*backgroundShell
}

type backgroundShell struct {
	id string

	cmd    *exec.Cmd
	doneCh chan struct{}
	cancel context.CancelFunc

	mu         sync.Mutex
	out        bytes.Buffer
	readOffset int
	running    bool
}

// Option represents a configuration option for the bash_tool.
type Option func(*Tool)

// New creates a new instance of the bash_tool.
func New(options ...Option) (*Tool, error) {
	name := "bash"

	bashTool := &Tool{
		defaultTimeout: DefaultTimeout,
		maxTimeout:     MaxTimeout,
		maxOutputChars: MaxOutputChars,
		runner:         LocalRunner{},
		shell:          map[string]*backgroundShell{},
	}

	for _, option := range options {
		option(bashTool)
	}

	description := "Executes a given bash command with optional timeout and background execution. " +
		"Each command starts in a fresh shell: cd and exported variables do not carry over."
	if bashTool.persistent {
		description = "Executes a given bash command in a persistent shell session with optional timeout and " +
			"background execution: the working directory and exported variables carry over between commands."
	}

	tool, err := llm.NewTool(
		name,
		description,
		bashTool.run,
	)
	if err != nil {
		return nil, err
	}

	outputTool, err := llm.NewTool(
		"bash_output",
		"Retrieves output from a running or completed background bash shell.",
		bashTool.output,
	)
	if err != nil {
		return nil, err
	}

	killTool, err := llm.NewTool(
		"kill_shell",
		"Kills a running background bash shell by its ID.",
		bashTool.kill,
	)
	if err != nil {
		return nil, err
	}

	bashTool.tool = tool
	bashTool.outputTool = outputTool
	bashTool.killTool = killTool

	return bashTool, nil
}

// Tool returns the llm.Tool representation.
func (t *Tool) Tool() *llm.Tool {
	return t.tool
}

// OutputTool returns the llm.Tool that retrieves incremental output from
// background shells started by Tool.
func (t *Tool) OutputTool() *llm.Tool {
	return t.outputTool
}

// KillTool returns the llm.Tool that terminates background shells started by
// Tool.
func (t *Tool) KillTool() *llm.Tool {
	return t.killTool
}

// WithWorkingDirectory sets the directory in which bash commands are executed.
func WithWorkingDirectory(dir string) Option {
	return func(t *Tool) {
		t.workingDir = dir
	}
}

// WithRunner sets how commands are launched: LocalRunner (the default) runs
// them on the host; NewBubblewrapRunner runs them in a sandbox.
func WithRunner(r Runner) Option {
	return func(t *Tool) {
		if r != nil {
			t.runner = r
		}
	}
}

// WithEnvAllowlist limits the environment commands get to the variables
// named, instead of the agent process's whole environment, which may hold
// secrets such as API keys. DefaultEnvAllowlist is a sane choice.
func WithEnvAllowlist(names ...string) Option {
	return func(t *Tool) {
		t.envAllowlist = append([]string{}, names...)
	}
}

// WithPersistentShell makes the working directory and the exported
// environment variables carry over from one command to the next, as in an
// interactive shell: after `cd sub`, the next command runs in sub. Each
// command still runs in its own process. A command that calls exit, times out
// or runs in the background does not change the carried state, and a
// background command starts from it. Commands run concurrently update it in
// the order they finish.
func WithPersistentShell() Option {
	return func(t *Tool) {
		t.persistent = true
	}
}

// WithEnv adds KEY=VALUE entries to the environment of every command.
func WithEnv(entries ...string) Option {
	return func(t *Tool) {
		t.extraEnv = append(t.extraEnv, entries...)
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

// WithDefaultTimeout sets the timeout used when an Input timeout is not
// provided and no explicit fixed timeout is configured.
func WithDefaultTimeout(d time.Duration) Option {
	return func(t *Tool) {
		t.defaultTimeout = d
	}
}

// WithMaxTimeout sets the maximum allowed timeout for Input timeout values.
func WithMaxTimeout(d time.Duration) Option {
	return func(t *Tool) {
		t.maxTimeout = d
	}
}

// WithMaxOutputChars sets the output truncation threshold.
func WithMaxOutputChars(chars int) Option {
	return func(t *Tool) {
		t.maxOutputChars = chars
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

//nolint:gocognit
func (t *Tool) run(ctx context.Context, input *Input) (*Output, error) {
	if input == nil {
		return nil, ErrNilInput
	}

	if strings.TrimSpace(input.Command) == "" {
		return nil, ErrCommandRequired
	}

	if input.Timeout < 0 {
		return nil, ErrTimeoutTooLarge
	}

	if err := t.checkCommand(input.Command); err != nil {
		return nil, err
	}

	cmdTimeout, err := t.resolveTimeout(input.Timeout)
	if err != nil {
		return nil, err
	}

	if input.RunInBackground {
		shellID, startErr := t.startBackground(input.Command, cmdTimeout)
		if startErr != nil {
			return nil, startErr
		}

		return &Output{BashID: shellID, Running: true}, nil
	}

	return t.runForeground(ctx, input.Command, cmdTimeout)
}

// runForeground runs command and waits for it, killing it when timeout (if
// any) expires. A command that times out returns what it printed so far, and
// one that fails returns its output with its exit code; an error means the
// command could not run or ctx ended.
func (t *Tool) runForeground(ctx context.Context, command string, cmdTimeout time.Duration) (*Output, error) {
	runCtx := ctx

	if cmdTimeout > 0 {
		var cancel context.CancelFunc

		runCtx, cancel = context.WithTimeout(ctx, cmdTimeout)
		defer cancel()
	}

	script := command
	if t.persistent {
		script = persistentScript(command)
	}

	cmd, err := t.newCommand(runCtx, script)
	if err != nil {
		return nil, err
	}

	var report *stateReport
	if t.persistent {
		if report, err = attachStateReport(cmd); err != nil {
			return nil, err
		}
	}

	combined, err := cmd.CombinedOutput()
	if report != nil {
		t.recordState(cmd, report, runCtx.Err() == nil)
	}

	out, truncated := t.truncateOutput(string(combined))

	if err == nil {
		return &Output{Output: out, Running: false, Truncated: truncated}, nil
	}

	// The caller's context ended: surface that rather than the exit error.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// The command's own timeout expired: return what it printed so far, so
	// the model can see how far it got.
	if runCtx.Err() != nil {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}

		// The output is already truncated: the note goes after it, uncut.
		out += fmt.Sprintf("command timed out after %s", cmdTimeout)

		return &Output{Output: out, Truncated: truncated, TimedOut: true}, nil
	}

	// If the command executed but failed (non-zero exit), return output with an inline marker
	// so callers can infer failure without an explicit exit code field.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}

		// The output is already truncated: the note goes after it, uncut.
		out += fmt.Sprintf("exit code: %d", exitErr.ExitCode())

		return &Output{Output: out, Running: false, Truncated: truncated}, nil
	}

	// For execution failures (e.g., bash missing), surface the error.
	return nil, err
}

func (t *Tool) output(_ context.Context, input *OutputInput) (*OutputOutput, error) {
	if input == nil {
		return nil, ErrNilInput
	}

	if strings.TrimSpace(input.BashID) == "" {
		return nil, ErrBashIDRequired
	}

	t.mu.RLock()
	shell, ok := t.shell[input.BashID]
	t.mu.RUnlock()

	if !ok {
		return nil, ErrShellNotFound
	}

	var re *regexp.Regexp

	if strings.TrimSpace(input.Filter) != "" {
		compiled, err := regexp.Compile(input.Filter)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidOutputFilter, err)
		}

		re = compiled
	}

	shell.mu.Lock()

	raw := shell.out.String()
	if shell.readOffset > len(raw) {
		shell.readOffset = len(raw)
	}

	incremental := raw[shell.readOffset:]
	shell.readOffset = len(raw)
	running := shell.running
	shell.mu.Unlock()

	// A finished shell whose output has now been read in full is forgotten.
	if !running {
		t.mu.Lock()
		delete(t.shell, input.BashID)
		t.mu.Unlock()
	}

	if re != nil && incremental != "" {
		lines := strings.Split(incremental, "\n")

		filtered := make([]string, 0, len(lines))
		for _, line := range lines {
			if re.MatchString(line) {
				filtered = append(filtered, line)
			}
		}

		incremental = strings.Join(filtered, "\n")
	}

	trimmed, truncated := t.truncateOutput(incremental)

	return &OutputOutput{Output: trimmed, Running: running, Truncated: truncated}, nil
}

func (t *Tool) kill(_ context.Context, input *KillShellInput) (*KillShellOutput, error) {
	if input == nil {
		return nil, ErrNilInput
	}

	if strings.TrimSpace(input.ShellID) == "" {
		return nil, ErrShellIDRequired
	}

	t.mu.RLock()
	shell, ok := t.shell[input.ShellID]
	t.mu.RUnlock()

	if !ok {
		return nil, ErrShellNotFound
	}

	shell.mu.Lock()
	running := shell.running
	proc := shell.cmd.Process
	shell.mu.Unlock()

	if !running || proc == nil {
		return &KillShellOutput{Killed: false}, nil
	}

	if err := killProcessGroup(proc); err != nil {
		return nil, err
	}

	return &KillShellOutput{Killed: true}, nil
}

func (t *Tool) resolveTimeout(timeoutMs int) (time.Duration, error) {
	effective := t.defaultTimeout
	if t.timeout > 0 {
		effective = t.timeout
	}

	if timeoutMs > 0 {
		requested := time.Duration(timeoutMs) * time.Millisecond
		if t.maxTimeout > 0 && requested > t.maxTimeout {
			return 0, ErrTimeoutTooLarge
		}

		effective = requested
	}

	if t.timeout > 0 && effective > t.timeout {
		effective = t.timeout
	}

	if t.maxTimeout > 0 && effective > t.maxTimeout {
		effective = t.maxTimeout
	}

	return effective, nil
}

// checkCommand applies the blocklist and the allowlist, case-insensitively,
// to command.
func (t *Tool) checkCommand(command string) error {
	lower := strings.ToLower(command)
	for _, pat := range t.blocklist {
		if strings.Contains(lower, pat) {
			return ErrCommandBlocked
		}
	}

	if len(t.allowlist) == 0 {
		return nil
	}

	for _, pat := range t.allowlist {
		if strings.Contains(lower, pat) {
			return nil
		}
	}

	return ErrCommandNotAllowed
}

// truncateOutput cuts output longer than maxOutputChars bytes to its first and
// last halves, joined by a marker saying how many bytes were cut. The cuts
// fall on UTF-8 character boundaries.
func (t *Tool) truncateOutput(output string) (string, bool) {
	if t.maxOutputChars <= 0 || len(output) <= t.maxOutputChars {
		return output, false
	}

	head := runeBoundary(output, t.maxOutputChars/2) //nolint:mnd // half for the start, half for the end
	tail := len(output) - (t.maxOutputChars - head)

	// Move the tail's start forward to the next character boundary.
	for tail < len(output) && !utf8.RuneStart(output[tail]) {
		tail++
	}

	return output[:head] + fmt.Sprintf("\n[... %d bytes truncated ...]\n", tail-head) + output[tail:], true
}

// runeBoundary returns the largest index i <= n where a UTF-8 character of s
// starts, so s[:i] does not split one.
func runeBoundary(s string, n int) int {
	for n > 0 && n < len(s) && !utf8.RuneStart(s[n]) {
		n--
	}

	return n
}

func (t *Tool) startBackground(command string, timeout time.Duration) (string, error) {
	shellID, err := randomID()
	if err != nil {
		return "", err
	}

	baseCtx := context.Background()

	var cancel context.CancelFunc
	if timeout > 0 {
		baseCtx, cancel = context.WithTimeout(context.Background(), timeout)
	}

	cmd, err := t.newCommand(baseCtx, command)
	if err != nil {
		if cancel != nil {
			cancel()
		}

		return "", err
	}

	shell := &backgroundShell{
		id:      shellID,
		cmd:     cmd,
		doneCh:  make(chan struct{}),
		cancel:  cancel,
		running: true,
	}

	writer := &lockedWriter{shell: shell}
	cmd.Stdout = writer
	cmd.Stderr = writer

	if startErr := cmd.Start(); startErr != nil {
		return "", startErr
	}

	t.mu.Lock()
	t.shell[shellID] = shell
	t.mu.Unlock()

	go func() {
		_ = cmd.Wait()

		if shell.cancel != nil {
			shell.cancel()
		}

		shell.mu.Lock()
		shell.running = false
		shell.mu.Unlock()
		close(shell.doneCh)
	}()

	return shellID, nil
}

func randomID() (string, error) {
	b := make([]byte, randomIDBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b), nil
}

type lockedWriter struct {
	shell *backgroundShell
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.shell.mu.Lock()
	defer w.shell.mu.Unlock()

	return w.shell.out.Write(p)
}
