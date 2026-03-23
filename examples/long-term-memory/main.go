package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	qdrantapi "github.com/qdrant/go-client/qdrant"

	"github.com/henomis/phero/agent"
	embeddingopenai "github.com/henomis/phero/embedding/openai"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/openai"
	nestmemory "github.com/henomis/phero/memory"
	ragmemory "github.com/henomis/phero/memory/rag"
	"github.com/henomis/phero/rag"
	vsqdrant "github.com/henomis/phero/vectorstore/qdrant"
)

type TimeInput struct{}

type TimeOutput struct {
	CurrentTime string `json:"current_time" jsonschema:"description=The current local time in RFC3339 format"`
}

func main() {
	// Parse command-line flags
	var topK uint64
	var timeout time.Duration

	var qdrantHost string
	var qdrantPort int
	var qdrantAPIKey string
	var qdrantUseTLS bool
	var qdrantCollection string

	flag.Uint64Var(&topK, "topk", 5, "How many memory snippets to retrieve per turn")
	flag.DurationVar(&timeout, "timeout", 2*time.Minute, "Timeout for each chat turn")

	flag.StringVar(&qdrantHost, "qdrant-host", "localhost", "Qdrant host")
	flag.IntVar(&qdrantPort, "qdrant-port", 6334, "Qdrant gRPC port")
	flag.StringVar(&qdrantAPIKey, "qdrant-api-key", "", "Qdrant API key (optional)")
	flag.BoolVar(&qdrantUseTLS, "qdrant-tls", false, "Use TLS for Qdrant connection")
	flag.StringVar(&qdrantCollection, "qdrant-collection", "long_term_memory", "Qdrant collection name")
	flag.Parse()

	ctx := context.Background()

	// Build LLM client
	llmClient, llmInfo := buildLLMFromEnv()
	embedder, embedderInfo := buildEmbedderFromEnv()

	qdrantHost = strings.TrimSpace(qdrantHost)
	qdrantAPIKey = strings.TrimSpace(qdrantAPIKey)
	qdrantCollection = strings.TrimSpace(qdrantCollection)
	if qdrantHost == "" {
		fmt.Fprintln(os.Stderr, "missing -qdrant-host")
		os.Exit(2)
	}
	if qdrantCollection == "" {
		fmt.Fprintln(os.Stderr, "missing -qdrant-collection")
		os.Exit(2)
	}

	bootstrapCtx, bootstrapCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer bootstrapCancel()

	// Qdrant collections require a fixed vector size. Infer it from the embedder.
	vecs, err := embedder.Embed(bootstrapCtx, []string{"vector size probe"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to embed probe text (to infer vector size): %v\n", err)
		return
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		fmt.Fprintln(os.Stderr, "failed to infer vector size from embedder")
		return
	}
	vectorSize := uint64(len(vecs[0]))

	qc, err := qdrantapi.NewClient(&qdrantapi.Config{
		Host:   qdrantHost,
		Port:   qdrantPort,
		APIKey: qdrantAPIKey,
		UseTLS: qdrantUseTLS,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create qdrant client: %v\n", err)
		return
	}

	store, err := vsqdrant.New(
		qc,
		qdrantCollection,
		vsqdrant.WithVectorSize(vectorSize),
		vsqdrant.WithWait(true),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create qdrant vector store: %v\n", err)
		return
	}

	if err := store.EnsureCollection(bootstrapCtx); err != nil {
		fmt.Fprintf(os.Stderr, "failed to ensure qdrant collection: %v\n", err)
		return
	}

	ragEngine, err := rag.New(store, embedder, rag.WithTopK(topK))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create rag memory engine: %v\n", err)
		return
	}

	conversationMemory := ragmemory.New(ragEngine)

	a, err := agent.New(
		llmClient,
		"Long-Term Memory Assistant",
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
	fmt.Println("🤖 Long-Term Memory (RAG + Qdrant)")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("LLM: %s\n", llmInfo)
	fmt.Printf("Embedder: %s\n", embedderInfo)
	fmt.Printf("Memory: semantic (topk=%d)\n", topK)
	fmt.Printf("Qdrant: %s:%d tls=%v collection=%s\n", qdrantHost, qdrantPort, qdrantUseTLS, qdrantCollection)
	fmt.Printf("Vector size: %d\n", vectorSize)
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  /history <query>  - Retrieve relevant past snippets")
	fmt.Println("  /clear     - Clear conversation memory")
	fmt.Println("  /stats     - Show memory configuration")
	fmt.Println("  /exit      - Exit the program")
	fmt.Println()
	fmt.Println("Start chatting! The agent recalls relevant past snippets.")
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
			handleCommand(ctx, line, conversationMemory, topK, qdrantHost, qdrantPort, qdrantUseTLS, qdrantCollection, vectorSize)
			fmt.Print("\n> ")
			continue
		}

		// Run the agent
		turnCtx, cancel := context.WithTimeout(ctx, timeout)
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

func handleCommand(ctx context.Context, cmd string, mem nestmemory.Memory, topK uint64, qdrantHost string, qdrantPort int, qdrantUseTLS bool, qdrantCollection string, vectorSize uint64) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return
	}
	name := strings.ToLower(fields[0])
	args := fields[1:]

	switch name {
	case "/exit", "/quit", "/q":
		fmt.Println("\nGoodbye! 👋")
		os.Exit(0)

	case "/history", "/h":
		if len(args) == 0 {
			fmt.Println("\nUsage: /history <query>")
			fmt.Println("Example: /history my favorite color")
			return
		}

		query := strings.TrimSpace(strings.Join(args, " "))
		messages, err := mem.Retrieve(ctx, query)
		if err != nil {
			fmt.Printf("Error retrieving memory: %v\n", err)
			return
		}
		if len(messages) == 0 {
			fmt.Println("\nNo relevant memory found.")
			return
		}

		fmt.Printf("\n📜 Retrieved Memory (topk=%d):\n", topK)
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
		if err := mem.Clear(ctx); err != nil {
			fmt.Printf("\n❌ Error clearing memory: %v\n", err)
			return
		}
		fmt.Println("\n✨ Cleared semantic memory.")

	case "/stats", "/s":
		fmt.Println("\n📊 Memory Statistics:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("Type: semantic (RAG)")
		fmt.Printf("TopK per turn: %d\n", topK)
		fmt.Printf("Store: qdrant (%s:%d tls=%v collection=%s)\n", qdrantHost, qdrantPort, qdrantUseTLS, qdrantCollection)
		fmt.Printf("Vector size: %d\n", vectorSize)

	case "/help", "/?":
		fmt.Println("\n📚 Available Commands:")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("  /history <query>  - Retrieve relevant past snippets")
		fmt.Println("  /clear     - Clear conversation memory")
		fmt.Println("  /stats     - Show memory configuration")
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

func buildEmbedderFromEnv() (*embeddingopenai.Client, string) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))

	// If no key and no base URL are set, assume a local OpenAI-compatible server (e.g. Ollama).
	if apiKey == "" && baseURL == "" {
		baseURL = embeddingopenai.OllamaBaseURL
	}

	opts := []embeddingopenai.Option{}
	if baseURL != "" {
		opts = append(opts, embeddingopenai.WithBaseURL(baseURL))
	}
	client := embeddingopenai.New(apiKey, opts...)

	info := fmt.Sprintf("model=%s base_url=%s", embeddingopenai.DefaultModel, baseURL)
	if baseURL == "" {
		info = fmt.Sprintf("model=%s", embeddingopenai.DefaultModel)
	}

	return client, info
}
