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

// Command coding-agent is a small terminal coding agent: it reads, edits and
// tests the code in a workspace, running shell commands in a bubblewrap
// sandbox and asking before anything not allowed by its permission rules.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/openai"
	"github.com/henomis/phero/v2/memory/jsonfile"
	"github.com/henomis/phero/v2/tool/bash"
	"github.com/henomis/phero/v2/tool/file"
	"github.com/henomis/phero/v2/tool/permission"
	"github.com/henomis/phero/v2/tool/todo"
)

const basePrompt = `You are a coding agent working in the repository at %s.
Read code before you change it, make small focused edits, and run the tests after a change.
Use the todo_write tool to plan any task with three or more steps.
Shell commands run in a sandbox without network access.
When you are done, say briefly what you changed.`

// allowed are the calls that run without asking: reading, planning, reading
// background output and harmless commands. Everything else is asked.
var allowed = []string{
	"read", "glob", "grep", "todo_write", "bash_output", "kill_shell",
	"bash(ls)", "bash(ls *)", "bash(pwd)", "bash(cat *)",
	"bash(git status*)", "bash(git diff*)", "bash(git log*)",
	"bash(go build *)", "bash(go vet *)", "bash(go test *)",
}

type config struct {
	dir         string
	stateDir    string
	unsafeLocal bool
	contextSize int
	turnTimeout time.Duration
}

// maxOutputChars returns the cap on one read or command output: about a
// quarter of the context window, at roughly 3 characters per token, so that
// one tool result can't fill the window by itself.
func (c config) maxOutputChars() int {
	return min(c.contextSize*3/4, file.DefaultMaxOutputChars) //nolint:mnd // a quarter of the window, 3 chars per token
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var cfg config

	flag.StringVar(&cfg.dir, "dir", ".", "workspace the agent works in")
	flag.StringVar(&cfg.stateDir, "state", "", "directory of the session and the task list (default: in the user cache directory)")
	flag.BoolVar(&cfg.unsafeLocal, "unsafe-local", false, "run commands on the host when bubblewrap is unavailable")
	flag.IntVar(&cfg.contextSize, "context", 100_000, "model context window, in tokens")
	flag.DurationVar(&cfg.turnTimeout, "turn-timeout", 30*time.Minute, "deadline of one turn")
	flag.Parse()

	dir, err := filepath.Abs(cfg.dir)
	if err != nil {
		return err
	}

	cfg.dir = dir

	// The state lives outside the workspace, where the model can neither read
	// its own session nor change it.
	if cfg.stateDir == "" {
		if cfg.stateDir, err = defaultStateDir(dir); err != nil {
			return err
		}
	}

	if err = os.MkdirAll(cfg.stateDir, 0o700); err != nil {
		return err
	}

	// One goroutine reads stdin, so the prompt and the permission questions
	// never race for a line.
	lines := readLines()

	client, info := buildLLMFromEnv()

	bashTool, err := newBashTool(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = bashTool.Close() }()

	tools, err := newTools(cfg, bashTool)
	if err != nil {
		return err
	}

	policy, err := permission.New(
		permission.WithWorkingDir(dir),
		permission.WithAllow(allowed...),
		permission.WithAsker(asker(lines)),
	)
	if err != nil {
		return err
	}

	policy.Apply(tools...)

	a, err := newAgent(cfg, client, tools)
	if err != nil {
		return err
	}

	fmt.Printf("coding agent in %s (%s)\nstate in %s\ntype a request, or /exit. Ctrl-C stops a turn.\n",
		dir, info, cfg.stateDir)

	// Ctrl-C cancels the turn in progress, or quits at the prompt.
	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)

	for {
		fmt.Print("\n> ")

		var line string

		select {
		case l, ok := <-lines:
			if !ok {
				return nil
			}

			line = strings.TrimSpace(l)
		case <-sigs:
			fmt.Println()
			return nil
		}

		switch line {
		case "":
			continue
		case "/exit":
			return nil
		}

		turn(context.Background(), cfg, a, line, sigs)
	}
}

// turn runs one request, until the answer, Ctrl-C or the turn deadline.
func turn(ctx context.Context, cfg config, a *agent.Agent, input string, sigs <-chan os.Signal) {
	ctx, cancel := context.WithTimeout(ctx, cfg.turnTimeout)
	defer cancel()

	done := make(chan struct{})
	defer close(done)

	go func() {
		select {
		case <-sigs:
			cancel()
		case <-done:
		}
	}()

	for ev, err := range a.RunStream(ctx, llm.Text(input)) {
		if err != nil {
			if ctx.Err() != nil {
				fmt.Println("\n[turn stopped]")
			} else {
				fmt.Println("\n[error]", err)
			}

			return
		}

		printEvent(ev)
	}
}

func printEvent(ev agent.Event) {
	switch ev.Type {
	case agent.EventTextDelta:
		fmt.Print(ev.TextDelta)
	case agent.EventToolCall:
		fmt.Printf("\n→ %s %s\n", ev.ToolName, shorten(ev.ToolArgs, 120))
	case agent.EventToolResult:
		if ev.ToolError {
			fmt.Printf("← %s failed: %s\n", ev.ToolName, shorten(ev.ToolResult, 200))
		} else {
			fmt.Printf("← %s ok\n", ev.ToolName)
		}
	case agent.EventDone:
		fmt.Println()

		if ev.Result.FinishReason == llm.FinishLength {
			fmt.Println("[the answer was cut off at the output limit: ask the agent to continue]")
		}

		if s := ev.Result.Summary; s != nil {
			fmt.Printf("[%d llm calls, %d tool calls, %d/%d tokens]\n",
				s.LLMCalls, s.ToolCalls, s.Usage.InputTokens, s.Usage.OutputTokens)
		}
	case agent.EventReasoningDelta, agent.EventUsage:
	}
}

// defaultStateDir returns the state directory of the workspace dir, in the
// user cache directory: one per workspace, named after it.
func defaultStateDir(dir string) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(dir))

	return filepath.Join(cache, "phero-coding-agent", filepath.Base(dir)+"-"+hex.EncodeToString(sum[:4])), nil
}

// newBashTool returns the bash tool, in a bubblewrap sandbox unless
// -unsafe-local is set and bubblewrap is unavailable.
func newBashTool(cfg config) (*bash.Tool, error) {
	opts := []bash.Option{
		bash.WithWorkingDirectory(cfg.dir),
		bash.WithPersistentShell(),
		bash.WithMaxOutputChars(cfg.maxOutputChars()),
		// Keep secrets such as OPENAI_API_KEY out of the commands' environment.
		bash.WithEnvAllowlist(bash.DefaultEnvAllowlist...),
		bash.WithOutputHandler(func(_ context.Context, chunk []byte) {
			// Show a long command's progress, dimmed, while it runs.
			fmt.Fprintf(os.Stderr, "\x1b[2m%s\x1b[0m", chunk)
		}),
	}

	sandboxOpts, env := goToolchainPaths()

	runner, err := bash.NewBubblewrapRunner(cfg.dir, sandboxOpts...)

	switch {
	case err == nil:
		opts = append(opts, bash.WithRunner(runner), bash.WithEnv(env...))
	case errors.Is(err, bash.ErrBubblewrapNotFound) && cfg.unsafeLocal:
		fmt.Fprintln(os.Stderr, "WARNING: bubblewrap not found: commands run on the host, unsandboxed")
	case errors.Is(err, bash.ErrBubblewrapNotFound):
		return nil, errors.New("bubblewrap (bwrap) not found: install it, or pass -unsafe-local to run commands on the host")
	default:
		return nil, err
	}

	return bash.New(opts...)
}

// goToolchainPaths returns the sandbox mounts and environment the go command
// needs: the toolchain read-only, and the build and module caches writable,
// so go test works without network. It returns nothing when go is missing.
func goToolchainPaths() ([]bash.BubblewrapOption, []string) {
	out, err := exec.Command("go", "env", "-json", "GOROOT", "GOCACHE", "GOMODCACHE").Output()
	if err != nil {
		return nil, nil
	}

	var goEnv struct{ GOROOT, GOCACHE, GOMODCACHE string }
	if err = json.Unmarshal(out, &goEnv); err != nil {
		return nil, nil
	}

	for _, p := range []string{goEnv.GOCACHE, goEnv.GOMODCACHE} {
		_ = os.MkdirAll(p, 0o755) // a missing path can't be mounted
	}

	return []bash.BubblewrapOption{
			bash.WithReadOnlyPaths(goEnv.GOROOT),
			bash.WithWritablePaths(goEnv.GOCACHE, goEnv.GOMODCACHE),
		}, []string{
			"GOCACHE=" + goEnv.GOCACHE,
			"GOMODCACHE=" + goEnv.GOMODCACHE,
			"GOTOOLCHAIN=local", // no network: use the mounted toolchain and module cache
			"GOPROXY=off",
		}
}

// newTools returns every tool of the agent: bash, the file tools and the
// todo list.
func newTools(cfg config, bashTool *bash.Tool) ([]*llm.Tool, error) {
	// One tracker: edit and write refuse a file the model has not read, or
	// that changed since it read it.
	fileOpts := []file.Option{
		file.WithWorkingDirectory(cfg.dir),
		file.WithReadTracker(file.NewReadTracker()),
		file.WithMaxOutputChars(cfg.maxOutputChars()),
	}

	read, err := file.NewReadTool(fileOpts...)
	if err != nil {
		return nil, err
	}

	write, err := file.NewWriteTool(fileOpts...)
	if err != nil {
		return nil, err
	}

	edit, err := file.NewEditTool(fileOpts...)
	if err != nil {
		return nil, err
	}

	multiEdit, err := file.NewMultiEditTool(fileOpts...)
	if err != nil {
		return nil, err
	}

	glob, err := file.NewGlobTool(fileOpts...)
	if err != nil {
		return nil, err
	}

	grep, err := file.NewGrepTool(fileOpts...)
	if err != nil {
		return nil, err
	}

	todoTool, err := newTodoTool(filepath.Join(cfg.stateDir, "todo.json"))
	if err != nil {
		return nil, err
	}

	// Commands run one at a time, in order: with a persistent shell, the
	// next command starts where the previous one left off, and live output
	// can't print over a permission question.
	bashCall := bashTool.Tool().SetExclusive(true)

	return []*llm.Tool{
		bashCall, bashTool.OutputTool(), bashTool.KillTool(),
		read.Tool(), write.Tool(), edit.Tool(), multiEdit.Tool(), glob.Tool(), grep.Tool(),
		todoTool,
	}, nil
}

// newTodoTool returns the todo_write tool. The list is saved to path after
// every change and restored from it at start.
func newTodoTool(path string) (*llm.Tool, error) {
	var saved []todo.Item
	if data, err := os.ReadFile(path); err == nil { //nolint:gosec // our own state file
		if err = json.Unmarshal(data, &saved); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}

	if len(saved) > 0 {
		fmt.Printf("restored task list:\n%s\n", todo.Render(saved))
	}

	list := todo.New(
		todo.WithItems(saved),
		todo.WithOnChange(func(items []todo.Item) {
			fmt.Printf("\n%s\n", todo.Render(items))

			if data, err := json.Marshal(items); err == nil {
				_ = os.WriteFile(path, data, 0o600)
			}
		}),
	)

	return list.Tool()
}

// newAgent returns the agent, with its session kept in the state directory so
// a restart resumes it.
func newAgent(cfg config, client llm.LLM, tools []*llm.Tool) (*agent.Agent, error) {
	a, err := agent.New(client, "coder", systemPrompt(cfg.dir))
	if err != nil {
		return nil, err
	}

	for _, t := range tools {
		if err = a.AddTool(t); err != nil {
			return nil, err
		}
	}

	mem, err := jsonfile.New(filepath.Join(cfg.stateDir, "session.json"))
	if err != nil {
		return nil, err
	}

	a.SetMemory(mem)
	a.SetCheckpoint(true) // save after every tool round, not only at the end of a turn
	a.SetCompaction(agent.NewSummaryCompactor(client), cfg.contextSize*8/10)
	a.SetMaxIterations(0) // a coding task takes many steps: the turn deadline bounds it

	return a, nil
}

// systemPrompt returns the base prompt plus the project's instructions, from
// AGENTS.md or CLAUDE.md, when the workspace has them.
func systemPrompt(dir string) string {
	prompt := fmt.Sprintf(basePrompt, dir)

	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // the workspace's own file
		if err == nil {
			return prompt + "\n\nProject instructions (" + name + "):\n\n" + string(data)
		}
	}

	return prompt
}

// asker returns a permission.Asker that asks in the terminal.
func asker(lines <-chan string) permission.Asker {
	return func(ctx context.Context, req permission.Request) (permission.Answer, error) {
		fmt.Printf("\nallow %s: %s\n[y]es / [a]lways / [n]o: ", req.Tool, askSubject(req))

		select {
		case line, ok := <-lines:
			if !ok {
				return permission.Reject, nil
			}

			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes":
				return permission.AllowOnce, nil
			case "a", "always":
				return permission.AllowAlways, nil
			default:
				return permission.Reject, nil
			}
		case <-ctx.Done():
			return permission.Reject, ctx.Err()
		}
	}
}

// askSubject returns what a permission question shows: for bash the command
// line exactly as it will run, uncut, since the request's subject lists the
// parsed commands without the pipes and operators between them.
func askSubject(req permission.Request) string {
	var args struct {
		Command string `json:"command"`
	}

	if req.Tool == "bash" && json.Unmarshal([]byte(req.Arguments), &args) == nil && args.Command != "" {
		return "\n  " + strings.ReplaceAll(args.Command, "\n", "\n  ")
	}

	return req.Subject
}

// readLines sends each line of stdin on the returned channel, and closes it
// at the end of input.
func readLines() <-chan string {
	lines := make(chan string)

	go func() {
		defer close(lines)

		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	return lines
}

// shorten returns s on one line, cut to at most n runes.
func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}

	return s
}

func buildLLMFromEnv() (llm.LLM, string) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))

	// If no key and no base URL are set, assume a local OpenAI-compatible server (e.g. Ollama).
	if apiKey == "" && baseURL == "" {
		baseURL = openai.OllamaBaseURL
	}

	if model == "" {
		if baseURL == openai.OllamaBaseURL && apiKey == "" {
			model = "gpt-oss:20b-cloud"
		} else {
			model = openai.DefaultModel
		}
	}

	opts := []openai.Option{openai.WithModel(model)}
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}

	info := "model=" + model
	if baseURL != "" {
		info += " base_url=" + baseURL
	}

	return openai.New(apiKey, opts...), info
}
