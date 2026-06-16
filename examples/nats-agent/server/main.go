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

// server registers a Phero agent on NATS using the NATS Agent Protocol v0.3.
//
// Usage:
//
//	go run ./examples/nats-agent/server -owner=alice -name=demo
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	natsagent "github.com/henomis/phero/nats"
	"github.com/henomis/phero/trace/text"
)

func main() {
	natsURL := flag.String("nats-url", "", "NATS server URL (overrides NATS_URL env var; default nats://localhost:4222)")
	owner := flag.String("owner", "demo", "Agent owner — 4th token in the subject hierarchy")
	name := flag.String("name", "default", "Instance name — 5th token in the subject hierarchy")
	flag.Parse()

	url := resolveNATSURL(*natsURL)

	nc, err := nats.Connect(url)
	if err != nil {
		log.Fatalf("NATS connect %s: %v", url, err)
	}
	defer nc.Drain() //nolint:errcheck

	llmClient, llmInfo := buildLLMFromEnv()
	fmt.Printf("LLM:     %s\n", llmInfo)

	a, err := agent.New(llmClient, "nats-demo-agent",
		"You are a helpful assistant. Be concise and clear.")
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	a.SetTracer(text.New(os.Stderr))

	srv, err := natsagent.New(nc, a, *owner, *name,
		natsagent.WithAgentID("phero"),
		natsagent.WithSession(uuid.NewString()),
		natsagent.WithHeartbeatInterval(10*time.Second),
	)
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Subject: agents.prompt.phero.%s.%s\n", *owner, *name)
	fmt.Println("Press Ctrl-C to stop.")
	fmt.Println()

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func resolveNATSURL(flag string) string {
	if flag != "" {
		return flag
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
