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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	memory "github.com/henomis/phero/memory/simple"
)

type GoRunInput struct {
	Args []string `json:"args" jsonschema:"description=Arguments to pass to the 'go' command (e.g. ['test','./...'])."`
}

type GoRunResult struct {
	ExitCode int    `json:"exit_code" jsonschema:"description=Process exit code. 0 means success."`
	Output   string `json:"output" jsonschema:"description=Combined stdout/stderr output."`
	Error    string `json:"error,omitempty" jsonschema:"description=Non-empty if execution failed."`
}

func main() {
	var goal string
	var timeout time.Duration
	flag.StringVar(&goal, "goal", "Do a quick health-check of this repo: list Go packages and run Go tests, then summarize.", "High-level goal for the multi-agent workflow")
	flag.DurationVar(&timeout, "timeout", 8*time.Minute, "Overall timeout for the run")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	supervisor, err := buildSupervisorWithBlackboard(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent architecture example: supervisor + specialists + blackboard")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- goal:", goal)
	fmt.Println()

	out, err := supervisor.Run(ctx, goal)
	if err != nil {
		panic(err)
	}

	fmt.Println(out)
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
	client := openai.New(apiKey, opts...)

	info := fmt.Sprintf("model=%s base_url=%s", model, baseURL)
	if baseURL == "" {
		info = fmt.Sprintf("model=%s", model)
	}

	return client, info
}

func buildSupervisorWithBlackboard(llmClient llm.LLM) (*agent.Agent, error) {
	shared := memory.New(60)

	goTool, err := newGoRunTool()
	if err != nil {
		return nil, err
	}

	researcher, err := agent.New(llmClient, "Researcher Agent", strings.TrimSpace(`You are a researcher agent in a multi-agent system.

You have access to the tool run_go. Use it to collect concrete evidence.

Rules:
- Only run 'go list' and 'go test'.
- Prefer at most TWO tool calls total.
- For a repo health-check, run:
  1) go list ./...
  2) go test ./...
- After running commands, write a short findings note with:
  - packages: (any interesting info + package count if easy)
  - tests: (pass/fail + key failures)
  - raw: include the command outputs (truncate if huge)

Be factual; do not speculate.`))
	if err != nil {
		return nil, err
	}
	researcher.SetMemory(shared)
	if err := researcher.AddTool(goTool); err != nil {
		return nil, err
	}

	drafter, err := agent.New(llmClient, "Drafter Agent", strings.TrimSpace(`You are a technical writer agent.

Given the user's goal and the researcher's findings, write a concise report:
- What commands were run
- What the outputs show (grounded)
- Next suggested steps if something failed

Do not invent details. If the findings are missing, ask to re-run research.`))
	if err != nil {
		return nil, err
	}
	drafter.SetMemory(shared)

	critic, err := agent.New(llmClient, "Critic Agent", strings.TrimSpace(`You are a critic / verifier agent.

You will be given a draft report and the research findings.
Return:
- any claims not supported by the findings
- missing key observations
- a corrected, improved version of the report (keep it short)

Be direct and practical.`))
	if err != nil {
		return nil, err
	}
	critic.SetMemory(shared)

	researchTool, err := researcher.AsTool(
		"research_repo",
		"Delegate to the Researcher Agent to run safe 'go list'/'go test' and report findings.",
	)
	if err != nil {
		return nil, err
	}

	draftTool, err := drafter.AsTool(
		"draft_report",
		"Delegate to the Drafter Agent to turn findings into a concise technical report.",
	)
	if err != nil {
		return nil, err
	}

	critiqueTool, err := critic.AsTool(
		"critique_report",
		"Delegate to the Critic Agent to verify the draft against evidence and improve it.",
	)
	if err != nil {
		return nil, err
	}

	supervisor, err := agent.New(llmClient, "Supervisor Agent", strings.TrimSpace(`You are a Supervisor/Router agent orchestrating specialists in a blackboard-style multi-agent system.

You have tools that represent other agents:
- research_repo: runs safe Go commands and returns findings
- draft_report: writes a report based on findings
- critique_report: verifies the report against findings

Workflow (follow it):
1) Call research_repo once to gather evidence.
2) Call draft_report with the goal and the findings.
3) Call critique_report with the draft and the findings.
4) Output the final, corrected report.

Constraints:
- Do not run commands directly (only via research_repo).
- Keep the final report grounded in tool outputs.
- Keep it concise.`))
	if err != nil {
		return nil, err
	}
	supervisor.SetMemory(shared)

	if err := supervisor.AddTool(researchTool); err != nil {
		return nil, err
	}
	if err := supervisor.AddTool(draftTool); err != nil {
		return nil, err
	}
	if err := supervisor.AddTool(critiqueTool); err != nil {
		return nil, err
	}

	return supervisor, nil
}

func newGoRunTool() (*llm.Tool, error) {
	return llm.NewTool(
		"run_go",
		"Run the 'go' command with a restricted, safe argument list (only 'list' and 'test').",
		runGo,
	)
}

func runGo(ctx context.Context, in *GoRunInput) (*GoRunResult, error) {
	if in == nil {
		return &GoRunResult{ExitCode: 2, Error: "missing input"}, nil
	}
	if len(in.Args) == 0 {
		return &GoRunResult{ExitCode: 2, Error: "missing args"}, nil
	}

	sub := in.Args[0]
	if sub != "test" && sub != "list" {
		return &GoRunResult{ExitCode: 2, Error: fmt.Sprintf("unsupported go subcommand: %s", sub)}, nil
	}
	for _, a := range in.Args {
		if strings.Contains(a, "\n") || strings.Contains(a, "\r") {
			return &GoRunResult{ExitCode: 2, Error: "invalid args"}, nil
		}
	}

	goPath, err := exec.LookPath("go")
	if err != nil {
		//nolint:nilerr // this is a valid "error" case for the tool result, not an actual execution error
		return &GoRunResult{ExitCode: 127, Error: "go not found in PATH"}, nil
	}

	cmd := exec.CommandContext(ctx, goPath, in.Args...)
	out, err := cmd.CombinedOutput()
	res := &GoRunResult{ExitCode: 0, Output: string(out)}
	if err == nil {
		return res, nil
	}

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		res.Error = err.Error()
		return res, nil
	}

	res.ExitCode = 1
	res.Error = err.Error()
	return res, nil
}
