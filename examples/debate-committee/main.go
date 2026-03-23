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
)

type DebateInput struct {
	Question string `json:"question"`
	Goal     string `json:"goal"`
}

type DebateResult struct {
	Member string `json:"member"`
	Output string `json:"output"`
}

type Debater struct {
	Name  string
	Agent *agent.Agent
}

func main() {
	var question string
	var goal string
	var timeout time.Duration
	flag.StringVar(&question, "question", "Given this repo's agent framework, propose a safe multi-agent design to diagnose failing tests.", "Debate question")
	flag.StringVar(&goal, "goal", "Produce a single best answer grounded in the committee arguments.", "High-level goal for the debate")
	flag.DurationVar(&timeout, "timeout", 6*time.Minute, "Overall timeout for the run")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	committee, judge, err := buildDebateAgents(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent architecture example: debate committee + judge")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- question:", question)
	fmt.Println()

	debate := make([]DebateResult, 0, len(committee))
	for _, member := range committee {
		out, err := member.Agent.Run(ctx, question)
		if err != nil {
			panic(err)
		}
		debate = append(debate, DebateResult{Member: member.Name, Output: strings.TrimSpace(out.Content)})
	}

	for _, r := range debate {
		fmt.Printf("=== %s ===\n", r.Member)
		fmt.Println(r.Output)
		fmt.Println()
	}

	judgeInput := renderJudgeInput(goal, question, debate)
	final, err := judge.Run(ctx, judgeInput)
	if err != nil {
		panic(err)
	}

	fmt.Println("=== judge (final) ===")
	fmt.Println(strings.TrimSpace(final.Content))
}

func renderJudgeInput(goal, question string, debate []DebateResult) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "goal: %s\n", goal)
	fmt.Fprintf(b, "question: %s\n\n", question)
	fmt.Fprintf(b, "committee_arguments:\n")
	for i, r := range debate {
		fmt.Fprintf(b, "- member_%d: %s\n", i+1, r.Member)
		fmt.Fprintf(b, "  argument: |\n")
		for _, ln := range strings.Split(r.Output, "\n") {
			fmt.Fprintf(b, "    %s\n", ln)
		}
	}
	return b.String()
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

func buildDebateAgents(llmClient llm.LLM) (committee []Debater, judge *agent.Agent, err error) {
	// Common debate rules to keep outputs short and comparable.
	debateRules := strings.TrimSpace(`Rules:
- Stay focused on the question.
- Prefer concrete steps / designs over generic advice.
- Keep your response under 200 lines.
- Do not claim you executed anything; you are reasoning only.`)

	memberPrompts := []struct {
		name string
		sys  string
	}{
		{
			name: "Advocate",
			sys: strings.TrimSpace(`You are the Advocate in a debate committee.

Your job: propose the strongest, most practical approach that would work in most repos.

` + debateRules),
		},
		{
			name: "Skeptic",
			sys: strings.TrimSpace(`You are the Skeptic in a debate committee.

Your job: identify risks, hidden assumptions, and failure modes in typical approaches.
Offer concrete mitigations.

` + debateRules),
		},
		{
			name: "Minimalist",
			sys: strings.TrimSpace(`You are the Minimalist in a debate committee.

Your job: propose the simplest architecture that still meets the goal.
Prefer fewer agents, fewer moving parts, and safe deterministic steps.

` + debateRules),
		},
	}

	committee = make([]Debater, 0, len(memberPrompts))
	for _, mp := range memberPrompts {
		a, e := agent.New(llmClient, mp.name, mp.sys)
		if e != nil {
			return nil, nil, e
		}
		committee = append(committee, Debater{Name: mp.name, Agent: a})
	}

	judge, err = agent.New(llmClient, "Judge", strings.TrimSpace(`You are the Judge in a debate-committee multi-agent system.

You receive a goal, a question, and multiple committee arguments.

Tasks:
- Identify points of agreement and conflict.
- Call out any weak or unsupported claims.
- Produce a single best final answer that merges the strongest parts.

Constraints:
- Keep it concise and actionable.
- Do not mention internal roles ("Advocate", etc.) in the final answer.
- Do not invent tool outputs or execution results.`))
	if err != nil {
		return nil, nil, err
	}

	return committee, judge, nil
}
