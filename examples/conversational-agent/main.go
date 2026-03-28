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

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	memory "github.com/henomis/phero/memory/simple"
)

type TimeInput struct{}

type TimeOutput struct {
	CurrentTime string `json:"current_time" jsonschema:"description=The current local time in RFC3339 format"`
}

func main() {
	// Parse command-line flags
	enableSummarization := flag.Bool("summarize", false, "Enable automatic summarization of conversation history")
	summarizationThreshold := flag.Uint("summary-threshold", 8, "Number of messages before triggering summarization (only used with -summarize)")
	summarySize := flag.Uint("summary-size", 15, "Number of messages before triggering summarization (only used with -summarize)")
	maxMessages := flag.Uint("max-messages", 20, "Maximum number of messages to keep in memory")
	flag.Parse()

	ctx := context.Background()

	// Build LLM client
	llmClient, llmInfo := buildLLMFromEnv()

	// Create agent with memory
	var conversationMemory *memory.Memory
	if *enableSummarization {
		conversationMemory = memory.New(*maxMessages, memory.WithSummarization(llmClient, *summarizationThreshold, *summarySize))
	} else {
		conversationMemory = memory.New(*maxMessages)
	}

	a, err := agent.New(
		llmClient,
		"Conversational Assistant",
		"You are a helpful, friendly conversational assistant. Maintain context from previous messages in the conversation. Be concise but personable.",
	)
	if err != nil {
		panic(err)
	}

	a.SetMemory(conversationMemory)
	a.SetMaxIterations(10)

	// Add helpful tools
	timeTool, err := llm.NewTool(
		"get_current_time",
		"Get the current local time",
		getCurrentTime,
	)
	if err != nil {
		panic(err)
	}
	if err := a.AddTool(timeTool); err != nil {
		panic(err)
	}

	// Print welcome message
	fmt.Println("🤖 Conversational Agent (with Memory)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("LLM: %s\n", llmInfo)
	fmt.Printf("Memory: %d messages max\n", *maxMessages)
	if *enableSummarization {
		fmt.Printf("Summarization: enabled (threshold=%d, summary_size=%d)\n", *summarizationThreshold, *summarySize)
	} else {
		fmt.Println("Summarization: disabled")
	}
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  /history   - Show conversation history")
	fmt.Println("  /clear     - Clear conversation memory")
	fmt.Println("  /stats     - Show memory statistics")
	fmt.Println("  /exit      - Exit the program")
	fmt.Println()
	fmt.Println("Start chatting! The agent remembers previous messages.")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Print("\n> ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			fmt.Print("> ")
			continue
		}

		// Handle commands
		if strings.HasPrefix(line, "/") {
			handleCommand(ctx, line, conversationMemory)
			fmt.Print("\n> ")
			continue
		}

		// Run the agent
		turnCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		response, err := a.Run(turnCtx, line)
		cancel()

		if err != nil {
			fmt.Printf("\n❌ Error: %v\n", err)
		} else {
			fmt.Printf("\n%s\n", strings.TrimSpace(response.Content))
		}

		fmt.Print("\n> ")
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "\nError reading input: %v\n", err)
	}

	fmt.Println("\nGoodbye! 👋")
}

func handleCommand(ctx context.Context, cmd string, mem *memory.Memory) {
	switch strings.ToLower(cmd) {
	case "/exit", "/quit", "/q":
		fmt.Println("\nGoodbye! 👋")
		os.Exit(0)

	case "/history", "/h":
		messages, err := mem.Retrieve(ctx, "")
		if err != nil {
			fmt.Printf("Error retrieving memory: %v\n", err)
			return
		}
		if len(messages) == 0 {
			fmt.Println("\nNo conversation history yet.")
			return
		}

		fmt.Printf("\n📜 Conversation History (%d messages):\n", len(messages))
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		for i, msg := range messages {
			role := msg.Role
			content := strings.TrimSpace(msg.Content)

			// Limit content display length
			if len(content) > 200 {
				content = content[:200] + "..."
			}

			symbol := ""
			switch role {
			case llm.ChatMessageRoleUser:
				symbol = "👤"
			case llm.ChatMessageRoleAssistant:
				symbol = "🤖"
			case llm.ChatMessageRoleSystem:
				symbol = "⚙️"
			case llm.ChatMessageRoleTool:
				symbol = "🔧"
			}

			fmt.Printf("%3d. %s %s: %s\n", i+1, symbol, role, content)

			// Show tool calls if present
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					fmt.Printf("       🔧 Tool call: %s\n", tc.Function.Name)
				}
			}
		}

	case "/clear", "/c":
		previousCount := mem.Len()
		_ = mem.Clear(ctx)
		fmt.Printf("\n✨ Cleared %d messages from memory.\n", previousCount)

	case "/stats", "/s":
		msgCount := mem.Len()
		messages, err := mem.Retrieve(ctx, "")
		if err != nil {
			fmt.Printf("Error retrieving memory: %v\n", err)
			return
		}

		fmt.Println("\n📊 Memory Statistics:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("Total messages: %d\n", msgCount)

		if msgCount > 0 {
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
		fmt.Println("\n📚 Available Commands:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("  /history   - Show conversation history")
		fmt.Println("  /clear     - Clear conversation memory")
		fmt.Println("  /stats     - Show memory statistics")
		fmt.Println("  /help      - Show this help message")
		fmt.Println("  /exit      - Exit the program")

	default:
		fmt.Printf("\n❓ Unknown command: %s\n", cmd)
		fmt.Println("Type /help to see available commands.")
	}
}

func getCurrentTime(_ context.Context, _ *TimeInput) (*TimeOutput, error) {
	return &TimeOutput{
		CurrentTime: time.Now().Format(time.RFC3339),
	}, nil
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
