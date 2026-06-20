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
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	natsmemory "github.com/henomis/phero/memory/nats"
	"github.com/henomis/phero/trace"
)

func main() {
	sessionID := flag.String("session", "default", "Session ID — use the same value across runs to resume a conversation")
	natsURL := flag.String("nats-url", "", "NATS server URL (overrides NATS_URL env var; default nats://localhost:4222)")

	flag.Parse()

	ctx := context.Background()

	// Resolve NATS URL: flag > env > default.
	url := *natsURL
	if url == "" {
		url = os.Getenv("NATS_URL")
	}

	if url == "" {
		url = nats.DefaultURL
	}

	// Connect to NATS.
	nc, err := nats.Connect(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "NATS connect error: %v\n", err)
		os.Exit(1)
	}
	defer nc.Close()

	// Create (or bind to) a JetStream KV bucket.
	js, err := nc.JetStream()
	if err != nil {
		fmt.Fprintf(os.Stderr, "JetStream error: %v\n", err)
		os.Exit(1)
	}

	kv, err := js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket:      "phero_memory",
		Description: "Phero agent conversation memory",
	})
	if err != nil {
		// Bucket may already exist — bind to it instead.
		kv, err = js.KeyValue("phero_memory")
		if err != nil {
			fmt.Fprintf(os.Stderr, "KV bucket error: %v\n", err)
			os.Exit(1)
		}
	}

	// Build the NATS-backed memory.
	mem, err := natsmemory.New(kv, *sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Memory error: %v\n", err)
		os.Exit(1)
	}

	// Check whether the session already has stored messages.
	existing, err := mem.Retrieve(ctx, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Memory retrieve error: %v\n", err)
		os.Exit(1)
	}

	// Build LLM client.
	llmClient, llmInfo := buildLLMFromEnv()

	// Create the agent.
	a, err := agent.New(
		llmClient,
		"Conversational Assistant",
		"You are a helpful, friendly conversational assistant. Maintain context from previous messages in the conversation. Be concise but personable.",
	)
	if err != nil {
		panic(err)
	}

	a.SetMemory(mem)
	a.SetMaxIterations(10)

	// Print welcome banner.
	fmt.Println("NATS Memory Agent")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("LLM:     %s\n", llmInfo)
	fmt.Printf("NATS:    %s\n", url)
	fmt.Printf("Session: %s\n", *sessionID)

	if len(existing) > 0 {
		fmt.Printf("Resumed: %d message(s) restored from NATS\n", len(existing))
	}

	fmt.Println()
	fmt.Println("Commands: /history  /clear  /stats  /exit")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Print("\n> ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			fmt.Print("> ")
			continue
		}

		if strings.HasPrefix(line, "/") {
			handleCommand(ctx, line, mem)
			fmt.Print("\n> ")

			continue
		}

		turnCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		response, err := a.Run(turnCtx, llm.Text(line))

		cancel()

		if err != nil {
			fmt.Printf("\nError: %v\n", err)
		} else {
			fmt.Printf("\n%s\n", strings.TrimSpace(response.TextContent()))
			printRunSummary(response.Summary)
		}

		fmt.Print("\n> ")
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\nError reading input: %v\n", err)
	}

	fmt.Println("\nGoodbye!")
}

func handleCommand(ctx context.Context, cmd string, mem *natsmemory.Memory) {
	switch strings.ToLower(cmd) {
	case "/exit", "/quit", "/q":
		fmt.Println("\nGoodbye!")
		os.Exit(0)

	case "/history", "/h":
		messages, err := mem.Retrieve(ctx, "")
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		if len(messages) == 0 {
			fmt.Println("\nNo conversation history yet.")
			return
		}

		fmt.Printf("\nConversation History (%d messages):\n", len(messages))
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		for i, msg := range messages {
			content := strings.TrimSpace(msg.TextContent())
			if len(content) > 200 {
				content = content[:200] + "..."
			}

			symbol := roleSymbol(msg.Role)
			fmt.Printf("%3d. %s %s: %s\n", i+1, symbol, msg.Role, content)
		}

	case "/clear", "/c":
		if err := mem.Clear(ctx); err != nil {
			fmt.Printf("Error clearing memory: %v\n", err)
			return
		}

		fmt.Println("\nMemory cleared.")

	case "/stats", "/s":
		messages, err := mem.Retrieve(ctx, "")
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}

		fmt.Println("\nMemory Statistics:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("Total messages: %d\n", len(messages))

		if len(messages) > 0 {
			roleCount := make(map[string]int)
			for _, msg := range messages {
				roleCount[msg.Role]++
			}

			fmt.Println("\nBy role:")

			for role, count := range roleCount {
				fmt.Printf("  %s: %d\n", role, count)
			}
		}

	case "/help", "/?":
		fmt.Println("\nAvailable Commands:")
		fmt.Println("  /history   Show conversation history")
		fmt.Println("  /clear     Clear all messages for this session")
		fmt.Println("  /stats     Show message count by role")
		fmt.Println("  /help      Show this help message")
		fmt.Println("  /exit      Exit")

	default:
		fmt.Printf("\nUnknown command: %s\n", cmd)
		fmt.Println("Type /help to see available commands.")
	}
}

func roleSymbol(role string) string {
	switch role {
	case llm.RoleUser:
		return ">"
	case llm.RoleAssistant:
		return "*"
	case llm.RoleSystem:
		return "#"
	default:
		return "-"
	}
}

func printRunSummary(summary *trace.RunSummary) {
	if summary == nil {
		return
	}

	fmt.Printf(
		"\n[summary: iter=%d llm=%d tools=%d mem=%d/%d tokens=%d/%d latency=%s]\n",
		summary.Iterations,
		summary.LLMCalls,
		summary.ToolCalls,
		summary.MemoryRetrieved,
		summary.MemorySaved,
		summary.Usage.InputTokens,
		summary.Usage.OutputTokens,
		summary.Latency.Total.Round(time.Millisecond),
	)
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

	info := fmt.Sprintf("model=%s", model)
	if baseURL != "" {
		info = fmt.Sprintf("model=%s base_url=%s", model, baseURL)
	}

	return client, info
}
