package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	qdrantapi "github.com/qdrant/go-client/qdrant"

	"github.com/henomis/phero/agent"
	embeddingopenai "github.com/henomis/phero/embedding/openai"
	"github.com/henomis/phero/llm"
	llmopenai "github.com/henomis/phero/llm/openai"
	memory "github.com/henomis/phero/memory/simple"
	"github.com/henomis/phero/rag"
	"github.com/henomis/phero/textsplitter"
	vsqdrant "github.com/henomis/phero/vectorstore/qdrant"
)

func main() {
	var filePath string
	var chunkSize int
	var chunkOverlap int
	var topK uint64
	var timeout time.Duration

	var qdrantHost string
	var qdrantPort int
	var qdrantAPIKey string
	var qdrantUseTLS bool
	var qdrantCollection string

	flag.StringVar(&filePath, "file", "", "Path to a .txt file to load as the knowledge base (required)")
	flag.IntVar(&chunkSize, "chunk-size", 1000, "Chunk size for splitting (measured in bytes)")
	flag.IntVar(&chunkOverlap, "chunk-overlap", 200, "Chunk overlap for splitting (measured in bytes)")
	flag.Uint64Var(&topK, "topk", 5, "How many chunks to retrieve per search")
	flag.DurationVar(&timeout, "timeout", 2*time.Minute, "Timeout for each chat turn")

	flag.StringVar(&qdrantHost, "qdrant-host", "localhost", "Qdrant host")
	flag.IntVar(&qdrantPort, "qdrant-port", 6334, "Qdrant gRPC port")
	flag.StringVar(&qdrantAPIKey, "qdrant-api-key", "", "Qdrant API key (optional)")
	flag.BoolVar(&qdrantUseTLS, "qdrant-tls", false, "Use TLS for Qdrant connection")
	flag.StringVar(&qdrantCollection, "qdrant-collection", "rag_chatbot", "Qdrant collection name")

	flag.Parse()

	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		fmt.Fprintln(os.Stderr, "missing required -file")
		os.Exit(2)
	}

	b, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read file: %v\n", err)
		os.Exit(1)
	}

	splitter := textsplitter.NewRecursiveCharacterTextSplitter(chunkSize, chunkOverlap)
	chunks := compactStrings(splitter.SplitText(string(b)))
	if len(chunks) == 0 {
		fmt.Fprintln(os.Stderr, "no non-empty chunks produced by splitter")
		os.Exit(1)
	}

	llmClient, llmInfo := buildLLMFromEnv()
	embedder, embedderInfo := buildEmbedderFromEnv()

	bootstrapCtx, bootstrapCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer bootstrapCancel()

	// Qdrant collections require a fixed vector size. Infer it from the embedder.
	vecs, err := embedder.Embed(bootstrapCtx, []string{chunks[0]})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to embed first chunk (to infer vector size): %v\n", err)
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
		APIKey: strings.TrimSpace(qdrantAPIKey),
		UseTLS: qdrantUseTLS,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create qdrant client: %v\n", err)
		return
	}

	store, err := vsqdrant.New(
		qc,
		strings.TrimSpace(qdrantCollection),
		vsqdrant.WithVectorSize(vectorSize),
		vsqdrant.WithWait(true),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create qdrant vector store: %v\n", err)
		return
	}

	ragEngine, err := rag.New(store, embedder, rag.WithTopK(topK))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create rag engine: %v\n", err)
		return
	}

	if err := ragEngine.Ingest(bootstrapCtx, chunks); err != nil {
		fmt.Fprintf(os.Stderr, "failed to ingest chunks: %v\n", err)
		return
	}

	ragTool, err := ragEngine.AsTool(
		"search_document",
		"Search the loaded document for relevant excerpts.",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create rag tool: %v\n", err)
		return
	}

	sysPrompt := strings.TrimSpace(fmt.Sprintf(`You are a helpful chatbot that answers questions about a single document loaded from disk.

Rules:
- For any question that depends on the document, call the tool "search_document" first to retrieve relevant excerpts.
- Use the retrieved excerpts as your source of truth.
- If you cannot find supporting excerpts, say you don't know based on the document.
- Keep answers concise and quote short excerpts when helpful.

Document: %s`, filepath.Base(filePath)))

	a, err := agent.New(llmClient, "RAG Chatbot", sysPrompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create agent: %v\n", err)
		return
	}
	a.SetMaxIterations(8)
	a.SetMemory(memory.New(60))
	if err := a.AddTool(ragTool); err != nil {
		fmt.Fprintf(os.Stderr, "failed to add rag tool: %v\n", err)
		return
	}

	fmt.Println("rag-chatbot")
	fmt.Println("- llm:", llmInfo)
	fmt.Println("- embedder:", embedderInfo)
	fmt.Println("- file:", filePath)
	fmt.Println("- chunks:", len(chunks))
	fmt.Println("- qdrant:", fmt.Sprintf("%s:%d tls=%v collection=%s", qdrantHost, qdrantPort, qdrantUseTLS, qdrantCollection))
	fmt.Println("- vector_size:", vectorSize)
	fmt.Println()
	fmt.Println("Type your question and press Enter. Type 'exit' to quit.")
	fmt.Print("> ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Print("> ")
			continue
		}
		switch strings.ToLower(line) {
		case "exit", "quit":
			return
		}

		turnCtx, turnCancel := context.WithTimeout(context.Background(), timeout)
		out, err := a.Run(turnCtx, fmt.Sprintf("Question: %s", line))
		turnCancel()

		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		} else {
			fmt.Println(strings.TrimSpace(out.Content))
		}
		fmt.Println()
		fmt.Print("> ")
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "stdin error:", err)
		return
	}
}

func compactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func buildLLMFromEnv() (llm.LLM, string) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))

	// If no key and no base URL are set, assume a local OpenAI-compatible server (e.g. Ollama).
	if apiKey == "" && baseURL == "" {
		baseURL = llmopenai.OllamaBaseURL
	}

	if model == "" {
		if baseURL == llmopenai.OllamaBaseURL && apiKey == "" {
			model = "gpt-oss:20b-cloud"
		} else {
			model = llmopenai.DefaultModel
		}
	}

	opts := []llmopenai.Option{llmopenai.WithModel(model)}
	if baseURL != "" {
		opts = append(opts, llmopenai.WithBaseURL(baseURL))
	}
	client := llmopenai.New(apiKey, opts...)

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
