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
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
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

type consoleInteractor struct {
	reader *bufio.Reader
	writer io.Writer
}

func newConsoleInteractor(reader io.Reader, writer io.Writer) *consoleInteractor {
	return &consoleInteractor{
		reader: bufio.NewReader(reader),
		writer: writer,
	}
}

func main() {
	var (
		goal    string
		timeout time.Duration
	)

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
	interactor := newConsoleInteractor(os.Stdin, os.Stdout)

	humanTool, err := human.New(human.WithInteractor(interactor.Ask))
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
- user_interaction: use this to ask structured user questions before taking any consequential action.
- simulate_action: use this only after explicit user approval.

For each action you plan to execute, call user_interaction with exactly one question:
- header: "Approval"
- question: describe the action and end with a question mark.
- multiSelect: false
- options:
	1) label "Approve" description "Proceed with the proposed action"
	2) label "Skip" description "Skip this action"
	3) label "Modify" description "User wants a modified version first"
	4) label "Stop" description "Stop the remaining plan"

Interpret the tool output from answers["Approval"]:
- selection "Approve": call simulate_action.
- selection "Skip": skip this action.
- selection "Modify": revise action using the optional free-text field and ask again.
- selection "Stop": stop all remaining actions and summarize.

Never call simulate_action without an explicit "Approve" selection.`))
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

// Ask presents structured questions and returns selected option labels.
func (c *consoleInteractor) Ask(ctx context.Context, in *human.Input) (map[string]human.Answer, error) {
	_ = ctx

	answers := make(map[string]human.Answer, len(in.Questions))

	for _, question := range in.Questions {
		if _, err := fmt.Fprintf(c.writer, "\n[%s] %s\n", question.Header, question.Question); err != nil {
			return nil, err
		}

		for idx, option := range question.Options {
			if _, err := fmt.Fprintf(c.writer, "  %d) %s - %s\n", idx+1, option.Label, option.Description); err != nil {
				return nil, err
			}
		}

		if question.MultiSelect {
			if _, err := fmt.Fprint(c.writer, "Select one or more options (comma separated labels or numbers). Optional free text: other: <text>\n> "); err != nil {
				return nil, err
			}
		} else {
			if _, err := fmt.Fprint(c.writer, "Select one option (label or number). Optional free text: other: <text>\n> "); err != nil {
				return nil, err
			}
		}

		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		answer, err := parseAnswer(strings.TrimSpace(line), question)
		if err != nil {
			return nil, err
		}

		answers[question.Header] = answer
	}

	return answers, nil
}

func parseAnswer(raw string, question human.Question) (human.Answer, error) {
	result := human.Answer{}
	if raw == "" {
		return result, nil
	}

	parts := strings.Split(raw, ",")
	seen := map[string]struct{}{}

	for _, part := range parts {
		choice := strings.TrimSpace(part)
		if choice == "" {
			continue
		}

		if strings.HasPrefix(strings.ToLower(choice), "other:") {
			result.Other = strings.TrimSpace(choice[len("other:"):])
			continue
		}

		label, err := resolveOptionLabel(choice, question.Options)
		if err != nil {
			return human.Answer{}, err
		}

		normalized := strings.ToLower(label)
		if _, exists := seen[normalized]; exists {
			continue
		}

		seen[normalized] = struct{}{}

		result.Selections = append(result.Selections, label)
	}

	if !question.MultiSelect && len(result.Selections) > 1 {
		return human.Answer{}, fmt.Errorf("question %q allows only one selection", question.Header)
	}

	return result, nil
}

func resolveOptionLabel(choice string, options []human.Choice) (string, error) {
	index, err := strconv.Atoi(choice)
	if err == nil {
		if index < 1 || index > len(options) {
			return "", fmt.Errorf("invalid option index: %d", index)
		}

		return options[index-1].Label, nil
	}

	normalizedChoice := strings.ToLower(strings.TrimSpace(choice))
	for _, option := range options {
		if strings.ToLower(option.Label) == normalizedChoice {
			return option.Label, nil
		}
	}

	return "", fmt.Errorf("invalid option label: %q", choice)
}
