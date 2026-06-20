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

// Package main runs the Researcher A2A agent.
//
// The researcher takes a topic and produces structured research notes
// covering key facts, current developments, and open questions.
// It is part of the multi-agent newsroom example.
//
// Start with:
//
//	OPENAI_API_KEY=... go run main.go
//
// Listens on :8081.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"

	"github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/trace/text"
)

const addr = ":8081"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	llmClient := buildLLMFromEnv()

	researcher, err := agent.New(
		llmClient,
		"researcher",
		`You are a research specialist. When given a topic, produce concise research notes with:

1. Background & key facts
2. Current state and recent developments
3. Main perspectives or controversies
4. Three to five open questions worth exploring

Format your notes clearly with numbered sections. Be factual and objective.`,
	)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	researcher.SetTracer(text.New(os.Stderr))

	pushStore := push.NewInMemoryStore()
	pushSender := push.NewHTTPPushSender(nil)
	taskStore := taskstore.NewInMemory(nil)

	srv, err := a2a.New(researcher, "http://localhost"+addr,
		a2a.WithVersion("1.0"),
		a2a.WithProvider("AI Newsroom", "http://localhost:8080"),
		a2a.WithStreaming(),
		a2a.WithRESTTransport(),
		a2a.WithTaskStore(taskStore),
		a2a.WithPushNotifications(pushStore, pushSender),
		a2a.WithSkills(sdka2a.AgentSkill{
			ID:          "topic-research",
			Name:        "Topic Research",
			Description: "Research a topic and produce structured notes with facts, context, and open questions.",
			Tags:        []string{"research", "analysis"},
		}),
	)
	if err != nil {
		log.Fatalf("create a2a server: %v", err)
	}

	mux := http.NewServeMux()
	srv.Mount(mux)

	httpServer := &http.Server{Addr: addr, Handler: mux}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("server error: %v", err)
		}
	}()

	fmt.Printf("researcher agent  addr=%s\n", addr)
	fmt.Printf("agent card        http://localhost%s/.well-known/agent-card.json\n", addr)
	fmt.Printf("json-rpc          http://localhost%s/\n", addr)
	fmt.Printf("rest/sse          http://localhost%s/rest/\n", addr)

	<-ctx.Done()

	if err := httpServer.Shutdown(context.Background()); err != nil {
		log.Printf("shutdown: %v", err)
	}
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
