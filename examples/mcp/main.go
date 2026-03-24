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
	"os/exec"
	"strings"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/mcp"
)

type Options[I, O any] struct {
	ClientName    string
	ClientVersion string
	Command       string
	Args          []string
	Toolname      string
	Input         *I
	Output        *O
}

func main() {
	llmClient, llmInfo := buildLLMFromEnv()

	ctx := context.Background()
	client := gomcp.NewClient(&gomcp.Implementation{Name: "myclient", Version: "1.0.0"}, nil)
	command := "./examples/mcp/server/server" // This command runs an MCP server that exposes a "get_random_quote" tool.
	cmd := exec.CommandContext(ctx, command)
	transport := &gomcp.CommandTransport{Command: cmd}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		panic(err)
	}
	defer func() {
		closeErr := session.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("mcp session close: %w", closeErr)
		}
	}()

	mcpServer := mcp.New(session)

	tools, err := mcpServer.AsTools(ctx, nil)
	if err != nil {
		panic(err)
	}

	a, err := agent.New(llmClient, "MCP Agent", "An agent that uses tools from an MCP server.")
	if err != nil {
		panic(err)
	}

	for _, tool := range tools {
		if err := a.AddTool(tool); err != nil {
			panic(err)
		}
	}

	res, err := a.Run(ctx, "Give me a random quote.")
	if err != nil {
		panic(err)
	}

	fmt.Printf("LLM used: %s\n", llmInfo)
	fmt.Printf("Agent response: %s\n", res)
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
