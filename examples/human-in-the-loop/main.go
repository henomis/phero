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
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/tool/human"
)

// ActionInput is the input for the simulate_action tool.
type ActionInput struct {
	Action      string `json:"action" jsonschema:"description=Short label for the action (e.g. 'create config file')."`
	Description string `json:"description" jsonschema:"description=Full description of what this action will do and why."`
}

// ActionOutput is the result returned by the simulate_action tool.
type ActionOutput struct {
	Status string `json:"status" jsonschema:"description=Outcome of the action: 'applied' or 'skipped'."`
}

func main() {
	var goal string
	var timeout time.Duration
	flag.StringVar(&goal, "goal",
		"Set up a new Go microservice project: create a module, add a README, add a Dockerfile, and configure a CI pipeline.",
		"High-level goal the agent should accomplish with human approval for each step")
	flag.DurationVar(&timeout, "timeout", 10*time.Minute, "Overall timeout for the run")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	a, err := buildAgent(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent architecture example: human-in-the-loop")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- goal:", goal)
	fmt.Println()
	fmt.Println("The agent will ask for your approval before each action.")
	fmt.Println("Type your response when prompted (accept / skip / modify / stop).")
	fmt.Println()

	out, err := a.Run(ctx, llm.Text(goal))
	if err != nil {
		panic(err)
	}

	fmt.Println()
	fmt.Println("=== agent summary ===")
	fmt.Println(strings.TrimSpace(out.TextContent()))
}

func buildLLMFromEnv() (llm.LLM, string) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))

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

func buildAgent(llmClient llm.LLM) (*agent.Agent, error) {
	humanTool, err := human.New()
	if err != nil {
		return nil, err
	}

	actionTool, err := llm.NewTool(
		"simulate_action",
		"Simulate executing an action that was approved by the human. Call this AFTER receiving human approval.",
		simulateAction,
	)
	if err != nil {
		return nil, err
	}

	a, err := agent.New(llmClient, "DevOps Assistant", strings.TrimSpace(`You are a DevOps assistant helping a developer set up a new project.

You have two tools:
- ask_human: use this to propose an action and ask for the developer's approval BEFORE doing anything.
- simulate_action: use this ONLY after the human has explicitly approved the action.

Workflow for every action you intend to take:
1. Call ask_human describing the action you plan to take and why.
2. Read the human's response carefully.
   - If they approve (e.g. "yes", "ok", "proceed", "accept"): call simulate_action.
   - If they decline (e.g. "no", "skip", "skip it"): skip this action and move on.
   - If they ask for a modification: adjust the action accordingly, then ask again before simulating.
   - If they say "stop" or "abort": stop all remaining actions and summarise what was completed.
3. Continue to the next action.

At the end, summarise which actions were applied and which were skipped.

Never simulate an action without explicit human approval.`))
	if err != nil {
		return nil, err
	}

	if err := a.AddTool(humanTool.Tool()); err != nil {
		return nil, err
	}

	if err := a.AddTool(actionTool); err != nil {
		return nil, err
	}

	a.SetMaxIterations(30)

	return a, nil
}

func simulateAction(_ context.Context, in *ActionInput) (*ActionOutput, error) {
	if in == nil {
		return &ActionOutput{Status: "skipped"}, nil
	}

	fmt.Printf("[action applied] %s - %s\n", in.Action, in.Description)

	return &ActionOutput{Status: "applied"}, nil
}
