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
	"github.com/henomis/phero/trace/text"
)

func main() {
	var goal string
	var timeout time.Duration
	flag.StringVar(&goal, "goal",
		"Produce a comprehensive briefing on the current state of quantum computing: cover the technology, recent breakthroughs, business landscape, and key challenges.",
		"High-level goal for the orchestrator to decompose and delegate")
	flag.DurationVar(&timeout, "timeout", 8*time.Minute, "Overall timeout for the run")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	orchestrator, err := buildOrchestrator(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent architecture example: orchestrator-workers (dynamic decomposition)")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- goal:", goal)
	fmt.Println()

	out, err := orchestrator.Run(ctx, llm.Text(goal))
	if err != nil {
		panic(err)
	}

	fmt.Println("=== final report ===")
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

func buildOrchestrator(llmClient llm.LLM) (*agent.Agent, error) {
	textTracer := text.New(os.Stdout)
	researcher, err := agent.New(llmClient, "Researcher", strings.TrimSpace(`You are a research specialist agent.

You receive a focused research question or subtask. Produce a concise, factual summary.
- Include concrete facts, dates, and examples where relevant.
- Limit your response to 200 words.
- Do not editorialize; stick to what is known.`))
	if err != nil {
		return nil, err
	}
	researcher.SetTracer(textTracer)

	writer, err := agent.New(llmClient, "Writer", strings.TrimSpace(`You are a technical writing specialist agent.

You receive a writing task and supporting research or notes.
Produce a clear, engaging, well-structured narrative.
- Use plain language suitable for a business/tech audience.
- Aim for 200-300 words unless instructed otherwise.
- Use concise paragraphs; no bullet lists unless explicitly requested.`))
	if err != nil {
		return nil, err
	}
	writer.SetTracer(textTracer)
	critic, err := agent.New(llmClient, "Critic", strings.TrimSpace(`You are a critical review specialist agent.

You receive a draft document and a review request.
Your job:
- Identify any factual gaps, unsupported claims, or unclear sections.
- Suggest concrete improvements.
- Produce a revised, improved version of the document.
Keep feedback direct and actionable.`))
	if err != nil {
		return nil, err
	}
	critic.SetTracer(textTracer)

	researchTool, err := researcher.AsTool(
		"research",
		"Delegate a focused research question to the Researcher worker. Returns a concise factual summary.",
	)
	if err != nil {
		return nil, err
	}

	writeTool, err := writer.AsTool(
		"write",
		"Delegate a writing task (with supporting notes) to the Writer worker. Returns a narrative draft.",
	)
	if err != nil {
		return nil, err
	}

	critiqueTool, err := critic.AsTool(
		"critique",
		"Delegate a review task to the Critic worker. Returns a revised, improved version of the provided document.",
	)
	if err != nil {
		return nil, err
	}

	orchestrator, err := agent.New(llmClient, "Orchestrator", strings.TrimSpace(`You are an Orchestrator agent that dynamically decomposes a high-level goal and delegates subtasks to specialist workers.

You have three worker tools:
- research: for gathering factual information on a specific question.
- write:    for turning notes or findings into readable narrative prose.
- critique: for reviewing and improving a draft document.

How to work:
1. Analyse the goal. Identify the subtasks needed - the exact steps depend on the goal, not a fixed template.
2. Delegate each subtask to the most appropriate worker tool, in whatever order makes sense.
3. You may call the same worker multiple times with different inputs if needed.
4. Once all subtasks are done, synthesise the results into a final report and output it.

Important:
- All decomposition decisions are yours - decide which workers to use and in what order based on the specific goal.
- Do not hard-code a fixed workflow; adapt to what the goal requires.
- Keep the final report concise and well-structured.`))
	if err != nil {
		return nil, err
	}

	if err := orchestrator.AddTool(researchTool); err != nil {
		return nil, err
	}

	if err := orchestrator.AddTool(writeTool); err != nil {
		return nil, err
	}

	if err := orchestrator.AddTool(critiqueTool); err != nil {
		return nil, err
	}

	orchestrator.SetMaxIterations(20)
	orchestrator.SetTracer(textTracer)

	return orchestrator, nil
}
