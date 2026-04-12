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
	"os"
	"strings"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/middleware"
	"github.com/henomis/phero/llm/openai"
	simplemem "github.com/henomis/phero/memory/simple"
)

func main() {
	var (
		seedFlag  string
		numAgents int
		numRounds int
		topk      int
		timeout   time.Duration
		interact  bool
	)

	flag.StringVar(&seedFlag, "seed",
		"A controversial new municipal policy proposes banning private gas vehicles from the city center by 2027. "+
			"Environmental groups, small business owners, commuters, taxi drivers, and tech startups are all reacting.",
		"Seed text or path to a seed document file describing the scenario to simulate")
	flag.IntVar(&numAgents, "agents", 8, "Number of persona agents to spawn (recommended max: 20)")
	flag.IntVar(&numRounds, "rounds", 5, "Number of simulation rounds")
	flag.IntVar(&topk, "topk", 15, "Number of recent feed entries visible to each agent per round")
	flag.DurationVar(&timeout, "timeout", 15*time.Minute, "Overall timeout for the full pipeline")
	flag.BoolVar(&interact, "interact", false, "Drop into interactive Q&A with the report agent after the simulation")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	llmClient, llmInfo := buildLLMFromEnv()
	rateLimiter, stop, err := middleware.NewLimiter(1, 4)
	if err != nil {
		panic(fmt.Errorf("rate limiter: %w", err))
	}
	defer stop()

	llmClient = llm.Use(llmClient, rateLimiter)
	seedText := readSeed(seedFlag)

	estimatedCalls := numAgents*numRounds + 3 // knowledge + personas + report
	fmt.Println("multi-agent architecture example: social simulation")
	fmt.Println("- llm:", llmInfo)
	fmt.Printf("- agents: %d  rounds: %d  topk: %d\n", numAgents, numRounds, topk)
	fmt.Printf("- estimated LLM calls: ~%d\n", estimatedCalls)
	fmt.Println()

	// Phase 1: Extract structured world facts from the seed material.
	fmt.Println("phase 1/4: extracting world facts...")

	worldFacts, err := extractWorldFacts(ctx, llmClient, seedText)
	if err != nil {
		panic(fmt.Errorf("world facts: %w", err))
	}

	fmt.Println("world facts extracted.")
	fmt.Println()

	// Phase 2: Generate personas grounded in the world facts.
	fmt.Printf("phase 2/4: generating %d personas...\n", numAgents)

	personas, err := generatePersonas(ctx, llmClient, worldFacts, numAgents)
	if err != nil {
		panic(fmt.Errorf("personas: %w", err))
	}

	fmt.Printf("%d personas generated:\n", len(personas))

	for _, p := range personas {
		fmt.Printf("  - %s (%s)\n", p.Name, p.Personality)
	}

	fmt.Println()

	// Phase 3: Build agents and run simulation rounds.
	paAgents := make([]*personaAgent, 0, len(personas))

	for _, p := range personas {
		pa, err := buildPersonaAgent(llmClient, p, numRounds)
		if err != nil {
			panic(err)
		}

		paAgents = append(paAgents, pa)
	}

	sim := newSimulation(paAgents, topk)

	fmt.Printf("phase 3/4: running %d simulation rounds...\n", numRounds)

	for round := 1; round <= numRounds; round++ {
		fmt.Printf("  round %d/%d\n", round, numRounds)

		err := sim.RunRound(ctx, round, numRounds, func(e FeedEntry) {
			fmt.Printf("    [%s] %s\n", e.Author, truncate(e.Post, 80))
		})
		if err != nil {
			panic(fmt.Errorf("round %d: %w", round, err))
		}
	}

	fmt.Println()

	// Phase 4: Synthesize a prediction report from the full transcript.
	fmt.Println("phase 4/4: generating simulation report...")

	reportAgent, err := buildReportAgent(llmClient)
	if err != nil {
		panic(err)
	}

	transcript := sim.Transcript()
	os.WriteFile("transcript.txt", []byte(transcript), 0o666)
	reportPrompt := fmt.Sprintf(
		"World facts:\n%s\n\nSimulation transcript:\n%s\n\nAnalyze this simulation and produce the report.",
		worldFacts, transcript,
	)

	reportResult, err := reportAgent.Run(ctx, llm.Text(reportPrompt))
	if err != nil {
		panic(fmt.Errorf("report agent: %w", err))
	}

	fmt.Println()
	fmt.Println("=== simulation report ===")
	fmt.Println(strings.TrimSpace(reportResult.TextContent()))

	// Optional phase 5: interactive Q&A with the report agent.
	if interact {
		fmt.Println()
		fmt.Println("=== interactive mode (type /exit to quit) ===")
		interactiveREPL(ctx, reportAgent)
	}
}

// extractWorldFacts runs a knowledge-extraction agent over seedText and returns
// a concise neutral summary of the key facts, entities, and tensions.
func extractWorldFacts(ctx context.Context, llmClient llm.LLM, seedText string) (string, error) {
	knowledgeAgent, err := agent.New(
		llmClient,
		"KnowledgeExtractor",
		strings.TrimSpace(`You are a knowledge extraction specialist.

Read the provided source material and produce a concise, neutral "world facts" summary (200–300 words) covering:
- The central situation, event, or topic
- Key entities (people, organizations, groups, policies) and their relationships
- Main tensions and conflicting interests
- Current state and open questions

Be factual and neutral. Do not take a stance.`),
	)
	if err != nil {
		return "", err
	}

	result, err := knowledgeAgent.Run(ctx, llm.Text("Extract world facts from this seed material:\n\n"+seedText))
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(result.TextContent()), nil
}

// buildReportAgent creates the analyst agent used to synthesize the simulation transcript
// into a structured prediction report. It uses memory so follow-up questions in the
// interactive REPL have full context.
func buildReportAgent(llmClient llm.LLM) (*agent.Agent, error) {
	a, err := agent.New(
		llmClient,
		"ReportAgent",
		strings.TrimSpace(`You are a simulation analyst specializing in emergent social dynamics.

You receive the full transcript of a multi-agent social simulation. Produce a structured report with these sections:

## Opinion Evolution
How did individual and group opinions shift round by round?

## Coalitions & Dynamics
Which agents aligned, which opposed, and what social dynamics emerged?

## Key Inflection Points
Which moments (cite round and agent) most significantly changed the conversation?

## Final Outlook
Based on the simulation, what is the most likely outcome or trajectory?

Be analytical. Cite specific agents by name and reference round numbers for key events.`),
	)
	if err != nil {
		return nil, err
	}

	// Large memory so the full transcript + report stay in context for the REPL.
	a.SetMemory(simplemem.New(100))

	return a, nil
}

// interactiveREPL runs a read-eval-print loop, passing each user line to reportAgent.
// The agent retains memory of the simulation transcript and its previous answers.
func interactiveREPL(ctx context.Context, reportAgent *agent.Agent) {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	fmt.Print("\n> ")

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			fmt.Print("> ")
			continue
		}

		if line == "/exit" || line == "/quit" || line == "/q" {
			break
		}

		turnCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		result, err := reportAgent.Run(turnCtx, llm.Text(line))
		cancel()

		if err != nil {
			fmt.Printf("error: %v\n", err)
		} else {
			fmt.Printf("\n%s\n", strings.TrimSpace(result.TextContent()))
		}

		fmt.Print("\n> ")
	}

	fmt.Println("\nGoodbye!")
}

// readSeed reads seed text from a file path. If the path cannot be opened,
// it treats the argument as inline text.
func readSeed(s string) string {
	data, err := os.ReadFile(s)
	if err == nil {
		return strings.TrimSpace(string(data))
	}

	return strings.TrimSpace(s)
}

// truncate shortens s to at most n characters (collapsing newlines) and appends "…".
func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")

	if len(s) <= n {
		return s
	}

	return s[:n-1] + "…"
}

// buildLLMFromEnv constructs an OpenAI-compatible LLM client from environment
// variables: OPENAI_API_KEY, OPENAI_BASE_URL, OPENAI_MODEL.
// When no key or base URL are set, it defaults to a local Ollama endpoint.
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
