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

// Package main demonstrates calling a remote A2A agent from a phero agent.
//
// It connects to the greeter server started by examples/a2a/server, wraps the
// remote agent as an llm.Tool, and runs a local orchestrator agent that
// delegates to it.
//
// Start the server first, then run this client:
//
// OPENAI_API_KEY=... go run main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/trace/text"
)

func main() {
	ctx := context.Background()
	serverURL := "http://localhost:8080"

	remoteClient, err := a2a.NewClient(ctx, serverURL)
	if err != nil {
		log.Fatalf("connect to remote agent: %v", err)
	}

	greeterTool, err := remoteClient.AsTool()
	if err != nil {
		log.Fatalf("build tool: %v", err)
	}

	// Create a local orchestrator agent and give it access to the remote greeter.
	llmClient, llmInfo := buildLLMFromEnv()

	orchestrator, err := agent.New(
		llmClient,
		"orchestrator",
		"You are an orchestrator. When asked to greet someone, use the greeter tool to generate the greeting.",
	)
	if err != nil {
		log.Fatalf("create orchestrator: %v", err)
	}
	orchestrator.SetTracer(text.New(os.Stderr))

	if err := orchestrator.AddTool(greeterTool); err != nil {
		log.Fatalf("add tool: %v", err)
	}

	fmt.Printf("LLM: %s\n", llmInfo)

	result, err := orchestrator.Run(ctx, "Please greet Alice via the remote greeter agent.")
	if err != nil {
		log.Fatalf("run: %v", err)
	}

	fmt.Println(result.Content)
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
