# Conversational Agent Example

An **interactive terminal chatbot** that demonstrates persistent multi-turn conversations with memory management.

This example shows:

- **REPL-style interaction** - Chat naturally in a loop
- **Memory management** - Conversation history persists across turns
- **Context awareness** - Agent remembers previous messages
- **Memory commands** - View history, clear memory, show stats
- **Run summaries** - Each turn prints aggregated iterations, memory activity, token counts, and latency

## What you'll learn

- How to use `memory.Memory` to maintain conversation context
- How to implement a chat loop with persistent state
- How to manage conversation history (view, clear, inspect)
- How memory affects agent responses

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama

go run ./examples/conversational-agent
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/conversational-agent
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

📈 Run summary: iterations=1 llm_calls=1 tool_calls=0 memory=4/2 tokens=154/21 latency=420ms
```

The agent maintains context from previous messages in the conversation.

### Commands

Type commands starting with `/` to manage memory:

- **`/history`** - Show full conversation history with role indicators
- **`/clear`** - Clear all conversation memory (fresh start)
- **`/stats`** - Show memory statistics (message count, capacity, usage)
- **`/help`** - Show available commands
- **`/exit`** - Exit the program

### Example session

```
> Tell me a joke about programming

Why do programmers prefer dark mode? Because light attracts bugs! 🐛

> /stats

📊 Memory Statistics:
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Total messages: 4
Max capacity: 100 messages
Usage: 4.0%

By role:
  system: 1
  user: 1
  assistant: 2

> /history

📜 Conversation History (4 messages):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  1. ⚙️ system: You are a helpful, friendly conversational assistant...
  2. 👤 user: Tell me a joke about programming
  3. 🤖 assistant: Why do programmers prefer dark mode?...
  4. 🤖 assistant: Why do programmers prefer dark mode? Because light attracts bugs! 🐛

> /clear

✨ Cleared 4 messages from memory.
```

## How it works

### Memory setup

```go
conversationMemory := memory.New(100) // Keep last 100 messages
a.SetMemory(conversationMemory)
```

The memory buffer keeps the most recent 100 messages. When full, the oldest messages are automatically removed (FIFO).

### Agent automatically uses memory

When you call `a.Run(ctx, userInput)`:

1. The agent prepends stored messages from memory
2. Adds the new user message
3. Calls the LLM with full conversation context
4. Stores the response back in memory
5. Returns the response to you

Memory is **automatic** - you don't need to manually manage the message history.

After each turn, the example also prints the `response.Summary` returned by the agent so you can inspect the cost and behavior of that single run.

### Memory inspection

```go
messages := mem.Messages()      // Get all stored messages
count := mem.Len()              // Get message count
mem.Clear()                     // Clear all messages
```

## Memory management strategies

### Buffer size considerations

- **Too small** (e.g., 10 messages) - Agent forgets context quickly
- **Too large** (e.g., 1000 messages) - May exceed LLM context window
- **Recommended** - 50-200 messages for most conversations

### When to clear memory

- Starting a new topic
- User requests a fresh start
- Agent seems confused by old context
- Testing/debugging

### Memory != Persistence

**Important:** Memory is in-RAM only. When the program exits, conversation history is lost.

For persistent storage, you would need to:
- Serialize `mem.Messages()` to disk/database
- Load messages back with `mem.Add(...)` on startup

## Customization ideas

Extend this example to learn more:

1. **Add save/load commands** - Persist conversations to JSON files
2. **Topic detection** - Auto-clear memory when topic changes
3. **Summarization** - Keep summaries when buffer is full
4. **More tools** - Weather, calculator, search, etc.
5. **Colored output** - Use ANSI colors for better readability
6. **Streaming responses** - Show partial responses as they're generated

## Comparison to other examples

| Example | Memory | Tools | Pattern |
|-|-|-|-|
| **Simple Agent** | ❌ No | ✅ Calculator | Single-turn |
| **Conversational Agent** | ✅ Yes | ✅ Time | Multi-turn REPL |
| **RAG Chatbot** | ✅ Yes | ✅ RAG search | Multi-turn + retrieval |
| **Multi-Agent** | ❌ No | ✅ Go commands | Sequential agents |

This example focuses specifically on **memory and conversational state**. It's the foundation for building chatbots, assistants, and interactive tools.

## Next steps

- [RAG Chatbot](../rag-chatbot/) - Add document retrieval to conversations
- [Supervisor Blackboard](../supervisor-blackboard/) - Shared memory between multiple agents
- [Skills](../skills/) - Add reusable capabilities to conversational agents
