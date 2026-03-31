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

// Package main runs a phero agent as an A2A server.
//
// The agent is exposed via the A2A JSON-RPC protocol. Any A2A-compatible
// client can discover it at /.well-known/agent-card.json and send tasks to
// the root endpoint.
//
// Usage:
//
// OPENAI_API_KEY=... go run main.go
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

	"github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	"github.com/henomis/phero/trace/text"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	llmClient, llmInfo := buildLLMFromEnv()

	greeter, err := agent.New(
		llmClient,
		"greeter",
		"You are a friendly greeter agent. When given a name, respond with a warm, personalised greeting.",
	)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}
	greeter.SetTracer(text.New(os.Stderr))

	srv, err := a2a.New(greeter, "http://localhost:8080")
	if err != nil {
		log.Fatalf("create a2a server: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/.well-known/agent-card.json", srv.AgentCardHandler())
	mux.Handle("/", srv.JSONRPCHandler())

	httpServer := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	errCh := make(chan error, 1)

	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	fmt.Println("A2A server listening on :8080")
	fmt.Printf("LLM: %s\n", llmInfo)
	fmt.Println("AgentCard: http://localhost:8080/.well-known/agent-card.json")

	select {
	case <-ctx.Done():
		if err := httpServer.Shutdown(context.Background()); err != nil {
			log.Printf("server shutdown: %v", err)
		}
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("server stopped: %v", err)
		}
	}
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
