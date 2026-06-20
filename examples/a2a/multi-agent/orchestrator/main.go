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
// It discovers the researcher (:8081), writer (:8082), and editor (:8083)
// A2A agents, wraps each one as an llm.Tool, and runs a local orchestrator
// agent that chains them together to produce a polished article on any topic.
//
// Pipeline:
//  1. researcher  → structured research notes
//  2. writer      → first draft article (from notes)
//  3. editor      → final polished article (from draft)
//
// Usage:
//
//	# Start the three servers first (each in its own terminal):
//	OPENAI_API_KEY=... go run ./examples/a2a/multi-agent/researcher
//	OPENAI_API_KEY=... go run ./examples/a2a/multi-agent/writer
//	OPENAI_API_KEY=... go run ./examples/a2a/multi-agent/editor
//
//	# Then run the orchestrator:
//	OPENAI_API_KEY=... go run ./examples/a2a/multi-agent/orchestrator -topic "quantum computing"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/trace/text"
)

func main() {
	topic := flag.String("topic", "quantum computing", "topic to research and write about")

	flag.Parse()

	ctx := context.Background()

	// ── Discover remote A2A agents ──────────────────────────────────────────
	researcherClient, err := a2a.NewClient(ctx, "http://localhost:8081",
		a2a.WithAcceptedOutputModes("text/plain"),
		a2a.WithPreferredTransports(sdka2a.TransportProtocolHTTPJSON, sdka2a.TransportProtocolJSONRPC),
	)
	if err != nil {
		log.Fatalf("connect to researcher: %v", err)
	}

	writerClient, err := a2a.NewClient(ctx, "http://localhost:8082",
		a2a.WithAcceptedOutputModes("text/plain"),
	)
	if err != nil {
		log.Fatalf("connect to writer: %v", err)
	}

	editorClient, err := a2a.NewClient(ctx, "http://localhost:8083",
		a2a.WithAcceptedOutputModes("text/plain"),
	)
	if err != nil {
		log.Fatalf("connect to editor: %v", err)
	}

	// ── Print discovered agent cards ────────────────────────────────────────
	printCard("researcher", researcherClient.Card())
	printCard("writer", writerClient.Card())
	printCard("editor", editorClient.Card())
	fmt.Println()

	// ── Wrap each remote agent as a local tool ──────────────────────────────
	researchTool, err := researcherClient.AsTool()
	if err != nil {
		log.Fatalf("build researcher tool: %v", err)
	}

	writeTool, err := writerClient.AsTool()
	if err != nil {
		log.Fatalf("build writer tool: %v", err)
	}

	editTool, err := editorClient.AsTool()
	if err != nil {
		log.Fatalf("build editor tool: %v", err)
	}

	// ── Build orchestrator agent ────────────────────────────────────────────
	llmClient := buildLLMFromEnv()

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

func printCard(label string, card *sdka2a.AgentCard) {
	skills := make([]string, 0, len(card.Skills))
	for _, s := range card.Skills {
		skills = append(skills, s.ID)
	}

	transports := make([]string, 0, len(card.SupportedInterfaces))
	for _, iface := range card.SupportedInterfaces {
		transports = append(transports, string(iface.ProtocolBinding))
	}

	fmt.Printf("%-12s v%-5s  skills=[%s]  transports=[%s]  streaming=%v  push=%v\n",
		label,
		card.Version,
		strings.Join(skills, ","),
		strings.Join(transports, ","),
		card.Capabilities.Streaming,
		card.Capabilities.PushNotifications,
	)
}

func buildLLMFromEnv() llm.LLM {
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

	return openai.New(apiKey, opts...)
}
