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
	"encoding/json"
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
)

type Plan struct {
	Goal  string     `json:"goal"`
	Steps []PlanStep `json:"steps"`
}

type PlanStep struct {
	Name        string   `json:"name"`
	Rationale   string   `json:"rationale"`
	GoArgs      []string `json:"go_args"`
	StopOnFail  bool     `json:"stop_on_fail"`
	ExpectNotes string   `json:"expect_notes"`
}

type StepResult struct {
	StepName string       `json:"step_name"`
	GoArgs   []string     `json:"go_args"`
	Result   *GoRunResult `json:"result"`
}

type RunSummaryInput struct {
	Goal        string       `json:"goal"`
	Plan        Plan         `json:"plan"`
	StepResults []StepResult `json:"step_results"`
}

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
	flag.StringVar(&goal, "goal", "Run the repository's Go tests and summarize any failures.", "High-level goal for the multi-agent workflow")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	planner, runner, analyst, critic, err := buildAgents(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent workflow:")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- goal:", goal)
	fmt.Println()

	plan, err := makePlan(ctx, planner, goal)
	if err != nil {
		panic(err)
	}

	stepResults, err := executePlan(ctx, runner, plan)
	if err != nil {
		panic(err)
	}

	analysis, err := synthesize(ctx, analyst, goal, plan, stepResults)
	if err != nil {
		panic(err)
	}

	review, err := reviewReport(ctx, critic, goal, plan, stepResults, analysis)
	if err != nil {
		panic(err)
	}

	fmt.Println("=== report ===")
	fmt.Println(analysis)
	fmt.Println()
	fmt.Println("=== critic ===")
	fmt.Println(review)
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

func buildAgents(llmClient llm.LLM) (planner, runner, analyst, critic *agent.Agent, err error) {
	planner, err = agent.New(llmClient, "Planner Agent", strings.TrimSpace(`You are a planning agent in a multi-agent system.

Return ONLY valid JSON, no markdown.

Your task: produce a short plan of safe, deterministic Go commands that help achieve the user's goal.

Constraints:
- Only use 'go list' and 'go test'.
- Each step must include go_args as an array of strings (without the leading 'go').
- Prefer at most 3 steps.

Output schema:
{
  "goal": "...",
  "steps": [
    {
      "name": "short title",
      "rationale": "why this step helps",
      "go_args": ["test", "./..."],
      "stop_on_fail": true,
      "expect_notes": "what evidence to look for"
    }
  ]
}`))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	runner, err = agent.New(llmClient, "Runner Agent", strings.TrimSpace(`You are an execution agent.

You have access to a tool that runs the Go command. Use it to execute the requested go_args.

Rules:
- Call the tool exactly once.
- Return ONLY the raw JSON object from the tool result, with no extra text and no markdown.`))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	analyst, err = agent.New(llmClient, "Analyst Agent", strings.TrimSpace(`You are an analyst agent.

You receive a goal, the plan, and the results of each executed step.
Write a concise technical report:
- What happened
- What failed (if anything) and likely causes
- Suggested next commands (still only go list/test) for deeper investigation
- Keep it grounded in the provided outputs; do not invent details.`))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	critic, err = agent.New(llmClient, "Critic Agent", strings.TrimSpace(`You are a critic agent.

Review the analyst report for:
- hallucinations / claims not supported by outputs
- missing key observations
- better next steps

Be direct and practical.`))
	if err != nil {
		return nil, nil, nil, nil, err
	}

	goTool, err := newGoRunTool()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := runner.AddTool(goTool); err != nil {
		return nil, nil, nil, nil, err
	}

	return planner, runner, analyst, critic, nil
}

func makePlan(ctx context.Context, planner *agent.Agent, goal string) (Plan, error) {
	response, err := planner.Run(ctx, goal)
	if err != nil {
		return Plan{}, err
	}

	out := extractJSONObject(response.Content)
	var plan Plan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		return Plan{}, fmt.Errorf("failed to parse planner JSON: %w\nraw=%s", err, out)
	}
	if strings.TrimSpace(plan.Goal) == "" {
		plan.Goal = goal
	}
	if len(plan.Steps) == 0 {
		return Plan{}, errors.New("planner returned an empty steps array")
	}
	if len(plan.Steps) > 3 {
		plan.Steps = plan.Steps[:3]
	}

	for i := range plan.Steps {
		if err := validateStep(plan.Steps[i]); err != nil {
			return Plan{}, fmt.Errorf("invalid plan step %d (%q): %w", i, plan.Steps[i].Name, err)
		}
	}

	return plan, nil
}

func validateStep(step PlanStep) error {
	if strings.TrimSpace(step.Name) == "" {
		return errors.New("missing name")
	}
	if len(step.GoArgs) == 0 {
		return errors.New("missing go_args")
	}
	sub := step.GoArgs[0]
	if sub != "test" && sub != "list" {
		return fmt.Errorf("unsupported go subcommand %q (allowed: test, list)", sub)
	}
	for _, a := range step.GoArgs {
		if strings.Contains(a, "\n") || strings.Contains(a, "\r") {
			return errors.New("go_args contains newline")
		}
	}
	return nil
}

func executePlan(ctx context.Context, runner *agent.Agent, plan Plan) ([]StepResult, error) {
	results := make([]StepResult, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		prompt := fmt.Sprintf("Execute this step now. step_name=%q go_args=%s", step.Name, mustJSON(step.GoArgs))
		response, err := runner.Run(ctx, prompt)
		if err != nil {
			return nil, err
		}

		out := extractJSONObject(response.Content)
		var r GoRunResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			return nil, fmt.Errorf("failed to parse runner JSON: %w\nraw=%s", err, out)
		}
		results = append(results, StepResult{StepName: step.Name, GoArgs: step.GoArgs, Result: &r})

		if step.StopOnFail && r.ExitCode != 0 {
			break
		}
	}
	return results, nil
}

func synthesize(ctx context.Context, analyst *agent.Agent, goal string, plan Plan, stepResults []StepResult) (string, error) {
	in := RunSummaryInput{Goal: goal, Plan: plan, StepResults: stepResults}
	response, err := analyst.Run(ctx, mustJSON(in))
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

func reviewReport(ctx context.Context, critic *agent.Agent, goal string, plan Plan, stepResults []StepResult, report string) (string, error) {
	in := struct {
		Goal        string       `json:"goal"`
		Plan        Plan         `json:"plan"`
		StepResults []StepResult `json:"step_results"`
		Report      string       `json:"report"`
	}{
		Goal:        goal,
		Plan:        plan,
		StepResults: stepResults,
		Report:      report,
	}

	response, err := critic.Run(ctx, mustJSON(in))
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b)
}

func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start == -1 || end == -1 || end <= start {
		return s
	}
	return strings.TrimSpace(s[start : end+1])
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
