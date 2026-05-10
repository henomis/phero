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

// Package main runs the Editor NATS agent.
//
// The editor reviews and polishes article drafts for clarity, style, and
// correctness. It is part of the multi-agent NATS newsroom example.
//
// Start with:
//
//	OPENAI_API_KEY=... go run ./examples/nats-agent/multi-agent/editor
//
// The agent registers under owner="newsroom", name="editor" on the NATS bus.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	natsagent "github.com/henomis/phero/nats"
	"github.com/henomis/phero/trace/text"
)

const (
	owner = "newsroom"
	name  = "editor"
)

func main() {
	natsURL := resolveNATSURL(os.Getenv("NATS_URL"))

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("NATS connect %s: %v", natsURL, err)
	}
	defer nc.Drain() //nolint:errcheck

	llmClient, llmInfo := buildLLMFromEnv()
	fmt.Printf("LLM:     %s\n", llmInfo)

	editor, err := agent.New(
		llmClient,
		"editor",
		`You are a meticulous copy editor. Review the given article draft and improve it by:

- Fixing grammar, style, and clarity issues
- Strengthening the opening hook and closing sentence
- Ensuring consistent tone and voice throughout
- Tightening verbose sentences without losing meaning
- Verifying all claims are clearly attributed

Return the complete polished article — not a list of changes.`,
	)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	editor.SetTracer(text.New(os.Stderr))

	srv, err := natsagent.New(nc, editor, owner, name,
		natsagent.WithAgentID("phero"),
		natsagent.WithVersion("1.0.0"),
		natsagent.WithHeartbeatInterval(10*time.Second),
	)
	if err != nil {
		log.Fatalf("create nats server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Subject: agents.prompt.phero.%s.%s\n", owner, name)
	fmt.Println("Press Ctrl-C to stop.")
	fmt.Println()

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func resolveNATSURL(envVal string) string {
	if envVal != "" {
		return envVal
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
