# Long-Term Memory Example (RAG + Qdrant)

An **interactive terminal chatbot** that demonstrates **semantic (RAG) memory** for multi-turn conversations.

This example shows:

- **REPL-style interaction** - Chat naturally in a loop
- **Semantic memory** - Retrieves *relevant* past snippets based on your current message
- **Vector store** - Memory is persisted in Qdrant
- **Memory commands** - Recall snippets, clear memory, show configuration

## What you'll learn

- How to use `memory/rag` to recall relevant past messages
- How to back semantic memory with `vectorstore/qdrant`
- How to implement a chat loop with persistent state
- How to manage semantic memory (recall, clear)
- How memory affects agent responses

## Run

This example uses an **embedding model** to store/retrieve memory.

You also need a **Qdrant** instance reachable via gRPC (default port `6334`). For local dev:

```bash
docker run --rm -p 6333:6333 -p 6334:6334 qdrant/qdrant
```

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama

go run ./examples/long-term-memory
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/long-term-memory
```

### Flags

```bash
go run ./examples/long-term-memory -- \
  -topk 5 \
  -qdrant-host localhost \
  -qdrant-port 6334 \
  -qdrant-collection long_term_memory
```

## Usage

### Chat normally

Just type your message and press Enter:

```
> What's your name?

I'm a conversational assistant. You can call me Assistant. How can I help you today?

> Remember that my favorite color is blue.

Got it! I'll remember that your favorite color is blue.

> What's my favorite color?

Your favorite color is blue!
```

With RAG memory, the agent recalls relevant past snippets based on what you ask.

### Commands

Type commands starting with `/` to manage memory:

- **`/history <query>`** - Retrieve relevant past snippets (semantic recall)
- **`/clear`** - Clear all conversation memory (fresh start)
- **`/stats`** - Show memory configuration
- **`/help`** - Show available commands
- **`/exit`** - Exit the program

### Example session

```
> Tell me a joke about programming

Why do programmers prefer dark mode? Because light attracts bugs! 🐛

> /stats

📊 Memory Statistics:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Type: semantic (RAG)
TopK per turn: 5
Store: qdrant (localhost:6334 tls=false collection=long_term_memory)
Vector size: 1536

> /history dark mode bugs

📜 Retrieved Memory (topk=5):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  1. ⚙️ system: Context retrieved from memory:
     user: Tell me a joke about programming
     assistant: Why do programmers prefer dark mode? Because light attracts bugs! 🐛

> /clear

✨ Cleared semantic memory.
```

## How it works

### Memory setup

This example wires an embedding model + vector store into a `rag.RAG`, then wraps it as an agent `memory.Memory`:

```go
qc, _ := qdrantapi.NewClient(&qdrantapi.Config{Host: "localhost", Port: 6334})

// Vector size must match the embedder output size (this example infers it at runtime).
store, _ := vsqdrant.New(qc, "long_term_memory", vsqdrant.WithVectorSize(1536))
ragEngine, _ := rag.New(store, embedder, rag.WithTopK(5))
conversationMemory := ragmemory.New(ragEngine)
a.SetMemory(conversationMemory)
```

### Agent automatically uses memory

When you call `a.Run(ctx, userInput)`:

1. The agent retrieves relevant memory snippets (semantic search)
2. Adds the new user message
3. Calls the LLM with full conversation context
4. Stores the response back in memory
5. Returns the response to you

Memory is **automatic** - you don't need to manually manage retrieval.

### A note about "history"

RAG memory is **not** a chronological transcript. `/history <query>` performs semantic retrieval, so it returns whatever past snippets are most relevant to the query.

## Memory management strategies

### Tuning tips

- Increase `-topk` if the agent often misses relevant earlier context.
- Keep `-topk` modest to avoid flooding the prompt with irrelevant memory.

### When to clear memory

- Starting a new topic
- User requests a fresh start
- Agent seems confused by old context
- Testing/debugging

### Persistence

This example persists embeddings and payloads to Qdrant, so memory survives process restarts.

## Customization ideas

Extend this example to learn more:

1. **Add save/load commands** - Persist conversations to JSON files
2. **Topic detection** - Auto-clear memory when topic changes
3. **More tools** - Weather, calculator, search, etc.
4. **Colored output** - Use ANSI colors for better readability
5. **Streaming responses** - Show partial responses as they're generated

## Comparison to other examples

| Example | Memory | Tools | Pattern |
|-|-|-|-|
| **Simple Agent** | ❌ No | ✅ Calculator | Single-turn |
| **Conversational Agent** | ✅ Yes | ✅ Time | Multi-turn REPL |
| **Long-Term Memory** | ✅ Yes (semantic) | ✅ Time | Multi-turn REPL + semantic recall |
| **RAG Chatbot** | ✅ Yes | ✅ RAG search | Multi-turn + retrieval |
| **Multi-Agent** | ❌ No | ✅ Go commands | Sequential agents |

This example focuses specifically on **memory and conversational state**. It's the foundation for building chatbots, assistants, and interactive tools.

## Next steps

- [RAG Chatbot](../rag-chatbot/) - Add document retrieval to conversations
- [Supervisor Blackboard](../supervisor-blackboard/) - Shared memory between multiple agents
- [Skills](../skills/) - Add reusable capabilities to conversational agents
