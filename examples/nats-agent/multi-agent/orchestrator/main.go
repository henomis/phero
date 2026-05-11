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

// Package main is the newsroom orchestrator.
//
// It discovers the researcher, writer, and editor agents on the NATS bus,
// wraps each one as an llm.Tool, and runs a local orchestrator agent that
// chains them together to produce a polished article on any topic.
//
// Pipeline:
//  1. researcher  → structured research notes
//  2. writer      → first draft article (from notes)
//  3. editor      → final polished article (from draft)
//
// Usage:
//
//	# Start the three agent servers first (each in its own terminal):
//	OPENAI_API_KEY=... go run ./examples/nats-agent/multi-agent/researcher
//	OPENAI_API_KEY=... go run ./examples/nats-agent/multi-agent/writer
//	OPENAI_API_KEY=... go run ./examples/nats-agent/multi-agent/editor
//
//	# Then run the orchestrator:
//	OPENAI_API_KEY=... go run ./examples/nats-agent/multi-agent/orchestrator -topic "quantum computing"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	natsagent "github.com/henomis/phero/nats"
	"github.com/henomis/phero/trace/text"
)

func main() {
	topic := flag.String("topic", "quantum computing", "topic to research and write about")
	natsURL := flag.String("nats-url", "", "NATS server URL (overrides NATS_URL env var; default nats://localhost:4222)")
	flag.Parse()

	url := resolveNATSURL(*natsURL)

	nc, err := nats.Connect(url)
	if err != nil {
		log.Fatalf("NATS connect %s: %v", url, err)
	}
	defer nc.Drain() //nolint:errcheck

	ctx := context.Background()

	c := natsagent.NewClient(nc,
		natsagent.WithDiscoveryTimeout(3*time.Second),
		natsagent.WithInactivityTimeout(120*time.Second),
	)

	// ── Discover the three newsroom agents ──────────────────────────────────
	researcher, err := discoverOne(ctx, c, "researcher")
	if err != nil {
		log.Fatalf("discover researcher: %v", err)
	}

	writer, err := discoverOne(ctx, c, "writer")
	if err != nil {
		log.Fatalf("discover writer: %v", err)
	}

	editor, err := discoverOne(ctx, c, "editor")
	if err != nil {
		log.Fatalf("discover editor: %v", err)
	}

	// ── Print discovered agents ──────────────────────────────────────────────
	printInfo("researcher", researcher)
	printInfo("writer", writer)
	printInfo("editor", editor)
	fmt.Println()

	// ── Wrap each remote agent as a local tool ──────────────────────────────
	researchTool, err := researcher.AsTool(
		"researcher",
		"Research a topic and produce structured notes with key facts, context, and open questions.",
	)
	if err != nil {
		log.Fatalf("build researcher tool: %v", err)
	}

	writeTool, err := writer.AsTool(
		"writer",
		"Write a clear, engaging 300-500 word article from the supplied research notes.",
	)
	if err != nil {
		log.Fatalf("build writer tool: %v", err)
	}

	editTool, err := editor.AsTool(
		"editor",
		"Edit and polish an article draft for grammar, clarity, style, and consistency.",
	)
	if err != nil {
		log.Fatalf("build editor tool: %v", err)
	}

	// ── Build orchestrator agent ────────────────────────────────────────────
	llmClient, llmInfo := buildLLMFromEnv()
	fmt.Printf("LLM: %s\n\n", llmInfo)

	orchestrator, err := agent.New(
		llmClient,
		"newsroom-orchestrator",
		`You are a newsroom orchestrator. Your job is to produce a final polished article on the given topic.

You MUST follow these steps in order:

Step 1 — Call the "researcher" tool with the topic to gather research notes.
Step 2 — Call the "writer" tool, passing the research notes, to produce a draft article.
Step 3 — Call the "editor" tool, passing the draft article, to get the final polished version.

Always pass the full output of each step as input to the next step.
Return the final polished article as your answer.`,
	)
	if err != nil {
		log.Fatalf("create orchestrator: %v", err)
	}

	orchestrator.SetTracer(text.New(os.Stderr))

	if err := orchestrator.AddTool(researchTool); err != nil {
		log.Fatalf("add researcher tool: %v", err)
	}

	if err := orchestrator.AddTool(writeTool); err != nil {
		log.Fatalf("add writer tool: %v", err)
	}

	if err := orchestrator.AddTool(editTool); err != nil {
		log.Fatalf("add editor tool: %v", err)
	}

	// ── Run the pipeline ────────────────────────────────────────────────────
	fmt.Printf("producing article on: %q\n\n", *topic)

	result, err := orchestrator.Run(ctx, llm.Text("Topic: "+*topic))
	if err != nil {
		log.Fatalf("orchestrator: %v", err)
	}

	fmt.Println("=== final article ===")
	fmt.Println(result.TextContent())
}

// discoverOne discovers the first agent on the bus matching owner="newsroom"
// and the given name, failing if none is found.
func discoverOne(ctx context.Context, c *natsagent.Client, agentName string) (*natsagent.AgentHandle, error) {
	handles, err := c.Discover(ctx,
		natsagent.FilterByOwner("newsroom"),
		natsagent.FilterByName(agentName),
	)
	if err != nil {
		return nil, err
	}
	return handles[0], nil
}

func printInfo(label string, info *natsagent.AgentHandle) {
	fmt.Printf("%-12s agent=%-8s owner=%-10s name=%-12s protocol=%s\n",
		label,
		info.Agent,
		info.Owner,
		info.Name,
		info.ProtocolVersion,
	)
}

func resolveNATSURL(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv("NATS_URL"); v != "" {
		return v
	}
	return nats.DefaultURL
}

// buildLLMFromEnv selects an LLM client from the available environment.
// Precedence: OPENAI_API_KEY / OPENAI_BASE_URL / OPENAI_MODEL → Ollama fallback.
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

	info := fmt.Sprintf("model=%s", model)
	if baseURL != "" {
		info = fmt.Sprintf("model=%s base_url=%s", model, baseURL)
	}
	return client, info
}
