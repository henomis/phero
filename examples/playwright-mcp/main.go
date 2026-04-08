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
	"fmt"
	"os"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	pheromcp "github.com/henomis/phero/mcp"
)

const mcpEndpoint = "http://localhost:8931/mcp"

func main() {
	llmClient, llmInfo := buildLLMFromEnv()

	ctx := context.Background()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "phero-playwright-client", Version: "1.0.0"}, nil)
	transport := &gomcp.StreamableClientTransport{Endpoint: mcpEndpoint}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		panic(err)
	}
	defer func() {
		if closeErr := session.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "mcp session close: %v\n", closeErr)
		}
	}()

	mcpServer := pheromcp.New(session)
	tools, err := mcpServer.AsTools(ctx, nil)
	if err != nil {
		panic(err)
	}

	a, err := agent.New(llmClient, "Playwright MCP Agent", "An agent that uses tools from a Playwright MCP server over HTTP.")
	if err != nil {
		panic(err)
	}

	for _, tool := range tools {
		if err := a.AddTool(tool); err != nil {
			panic(err)
		}
	}

	prompt := strings.TrimSpace(`
Use the available Playwright tools to do a simple smoke test:

1) Open https://example.com
2) Extract the page title and the visible H1 text

Return ONLY this format:
title=<...>
h1=<...>
`)

	res, err := a.Run(ctx, prompt)
	if err != nil {
		panic(err)
	}

	fmt.Printf("LLM used: %s\n", llmInfo)
	fmt.Printf("Agent response: %s\n", res.Content)
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
