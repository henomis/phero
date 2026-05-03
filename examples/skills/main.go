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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/tool/bash"
	"github.com/henomis/phero/tool/file"
	skilltool "github.com/henomis/phero/tool/skill"
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

	dispatcher, err := skilltool.New("./skills")
	if err != nil {
		panic(err)
	}

	a, err := agent.New(llmClient, "Agent", "An agent that helps create web pages and fetch random quotes")
	if err != nil {
		panic(err)
	}

	if err := a.AddTool(dispatcher.Tool()); err != nil {
		panic(err)
	}

	bashTool, err := bash.New()
	if err != nil {
		panic(err)
	}
	bashTool.Tool().Use(confirmBeforeRun(os.Stdin, os.Stdout))
	if err := a.AddTool(bashTool.Tool()); err != nil {
		panic(err)
	}

	readTool, err := file.NewReadTool()
	if err != nil {
		panic(err)
	}
	if err := a.AddTool(readTool.Tool()); err != nil {
		panic(err)
	}

	writeTool, err := file.NewWriteTool()
	if err != nil {
		panic(err)
	}
	writeTool.Tool().Use(confirmBeforeRun(os.Stdin, os.Stdout))
	if err := a.AddTool(writeTool.Tool()); err != nil {
		panic(err)
	}

	editTool, err := file.NewEditTool()
	if err != nil {
		panic(err)
	}
	editTool.Tool().Use(confirmBeforeRun(os.Stdin, os.Stdout))
	if err := a.AddTool(editTool.Tool()); err != nil {
		panic(err)
	}

	// a.SetTracer(text.New(os.Stderr))

	res, err := a.Run(ctx, llm.Text(`Your task:
1. Check if the file "/tmp/quote.html" exists.
2. If the file does not exist, create a valid HTML file at "/tmp/quote.html" with a <div id="quote"></div> element, and insert a random quote inside this div.
3. If the file already exists, update only the content inside the <div id="quote"></div> tags with a new random quote. Do not modify any other part of the file.
4. Ensure the quote is properly escaped for HTML.

Respond only with a summary of the action taken and the quote used. Do not include any code or file content in your response.
`))
	if err != nil {
		panic(err)
	}

	fmt.Printf("LLM used: %s\n", llmInfo)
	fmt.Printf("Agent response: %s\n", res.TextContent())
}

// ErrConfirmationDenied is returned when the user rejects a tool call.
var ErrConfirmationDenied = errors.New("user denied confirmation")

// confirmBeforeRun returns a ToolMiddleware that prints the tool name and
// arguments to w, reads a y/N answer from r, and short-circuits with
// ErrConfirmationDenied when the user does not confirm.
func confirmBeforeRun(r *os.File, w *os.File) llm.ToolMiddleware {
	scanner := bufio.NewScanner(r)
	return func(tool *llm.Tool, next llm.ToolHandler) llm.ToolHandler {
		return func(ctx context.Context, arguments string) (any, error) {
			_, _ = fmt.Fprintf(w, "\n[confirm] tool=%s args=%s\nProceed? [y/N] ", tool.Name(), arguments)
			if !scanner.Scan() {
				return nil, ErrConfirmationDenied
			}
			if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
				return nil, ErrConfirmationDenied
			}
			return next(ctx, arguments)
		}
	}
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
