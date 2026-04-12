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
	"sync"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
)

// workerResult holds the output or error from a single parallel worker agent.
type workerResult struct {
	angle  string
	output string
	err    error
}

func main() {
	var topic string
	var timeout time.Duration
	flag.StringVar(&topic, "topic", "renewable energy", "Research topic to investigate from multiple angles")
	flag.DurationVar(&timeout, "timeout", 6*time.Minute, "Overall timeout for the run")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()

	workers, synthesizer, err := buildAgents(llmClient)
	if err != nil {
		panic(err)
	}

	fmt.Println("multi-agent architecture example: parallel research (fan-out / fan-in)")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- topic:", topic)
	fmt.Println()

	// Fan-out: run all worker agents concurrently.
	results := make([]workerResult, len(workers))
	var wg sync.WaitGroup

	for i, entry := range workers {
		wg.Add(1)

		go func(idx int, angle string, a *agent.Agent) {
			defer wg.Done()

			prompt := fmt.Sprintf("Research the topic %q from the %s angle.", topic, angle)
			out, err := a.Run(ctx, llm.Text(prompt))
			if err != nil {
				results[idx] = workerResult{angle: angle, err: err}
				return
			}

			results[idx] = workerResult{angle: angle, output: strings.TrimSpace(out.TextContent())}
		}(i, entry.angle, entry.agent)
	}

	wg.Wait()

	// Check for worker errors and print individual outputs.
	for _, r := range results {
		if r.err != nil {
			panic(fmt.Errorf("worker %q failed: %w", r.angle, r.err))
		}

		fmt.Printf("=== %s ===\n%s\n\n", r.angle, r.output)
	}

	// Fan-in: build a combined prompt for the synthesizer.
	synthesisPrompt := buildSynthesisPrompt(topic, results)

	fmt.Println("synthesizing results...")
	fmt.Println()

	finalOut, err := synthesizer.Run(ctx, llm.Text(synthesisPrompt))
	if err != nil {
		panic(fmt.Errorf("synthesizer failed: %w", err))
	}

	fmt.Println("=== synthesis ===")
	fmt.Println(strings.TrimSpace(finalOut.TextContent()))
}

type worker struct {
	angle string
	agent *agent.Agent
}

func buildAgents(llmClient llm.LLM) ([]worker, *agent.Agent, error) {
	workerDefs := []struct {
		angle string
		sys   string
	}{
		{
			angle: "historical",
			sys: strings.TrimSpace(`You are a historical research agent.

Your role: provide a concise historical overview of the given topic.
- Cover origins, key milestones, and how understanding or adoption evolved.
- Stay factual and cite approximate dates where relevant.
- Limit your response to 150-200 words.`),
		},
		{
			angle: "technical",
			sys: strings.TrimSpace(`You are a technical research agent.

Your role: explain the technical mechanisms behind the given topic.
- Focus on how it works, key technologies, and engineering challenges.
- Keep it accessible but precise.
- Limit your response to 150-200 words.`),
		},
		{
			angle: "societal impact",
			sys: strings.TrimSpace(`You are a societal impact research agent.

Your role: analyze the real-world effects of the given topic on people, economies, and the environment.
- Highlight both benefits and risks.
- Reference concrete examples where possible.
- Limit your response to 150-200 words.`),
		},
	}

	workers := make([]worker, 0, len(workerDefs))
	for _, def := range workerDefs {
		a, err := agent.New(llmClient, def.angle+" Agent", def.sys)
		if err != nil {
			return nil, nil, err
		}

		workers = append(workers, worker{angle: def.angle, agent: a})
	}

	synthesizer, err := agent.New(llmClient, "Synthesizer Agent", strings.TrimSpace(`You are a synthesis agent in a multi-agent research system.

You receive research findings from multiple specialist agents, each covering a different angle of the same topic.

Your task:
- Integrate the findings into one coherent, well-structured report.
- Identify connections and tensions across the angles.
- Keep the final report to 300-400 words.
- Use clear section headers (Historical Overview, Technical Mechanisms, Societal Impact, Synthesis).`))
	if err != nil {
		return nil, nil, err
	}

	return workers, synthesizer, nil
}

func buildSynthesisPrompt(topic string, results []workerResult) string {
	b := &strings.Builder{}
	fmt.Fprintf(b, "topic: %s\n\n", topic)
	fmt.Fprintf(b, "research findings:\n")

	for _, r := range results {
		fmt.Fprintf(b, "\n--- %s ---\n%s\n", r.angle, r.output)
	}

	return b.String()
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
