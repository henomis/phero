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

// Package main runs the Writer A2A agent.
//
// The writer takes research notes and a topic, and produces a polished
// 300-500 word article. It is part of the multi-agent newsroom example.
//
// Start with:
//
//	OPENAI_API_KEY=... go run main.go
//
// Listens on :8082.
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

	"github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/trace/text"
)

const addr = ":8082"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	llmClient := buildLLMFromEnv()

	writer, err := agent.New(
		llmClient,
		"writer",
		`You are a skilled science and technology writer. Given research notes on a topic, write a clear,
engaging article of 300 to 500 words.

Structure: engaging opening → key findings → broader implications → memorable conclusion.
Use active voice, avoid jargon, and make complex ideas accessible to a general audience.`,
	)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	writer.SetTracer(text.New(os.Stderr))

	srv, err := a2a.New(writer, "http://localhost"+addr,
		a2a.WithVersion("1.0"),
		a2a.WithProvider("AI Newsroom", "http://localhost:8080"),
		a2a.WithStreaming(),
		a2a.WithSkills(sdka2a.AgentSkill{
			ID:          "article-writing",
			Name:        "Article Writing",
			Description: "Write a structured, readable article from research notes.",
			Tags:        []string{"writing", "content"},
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

	fmt.Printf("writer agent   addr=%s\n", addr)
	fmt.Printf("agent card     http://localhost%s/.well-known/agent-card.json\n", addr)

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
