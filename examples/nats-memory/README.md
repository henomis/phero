# NATS Memory Example

An **interactive terminal chatbot** that stores conversation history in **NATS JetStream Key-Value**, demonstrating persistent memory that survives process restarts.

This example shows:

- **Persistent memory** — conversation history is stored in NATS JetStream KV and reloaded on the next run
- **Session IDs** — run multiple named sessions side-by-side with `-session <id>`
- **REPL interaction** — chat naturally in a loop with `/history`, `/clear`, `/stats` commands

## What you'll learn

- How to use `memory/nats` to back an agent with NATS JetStream KV
- How to wire a `nats.KeyValue` bucket into `natsmemory.New`
- How session-scoped keys allow multiple independent conversations in the same bucket

## Requirements

- A running NATS server with JetStream enabled

## Start NATS

```bash
docker run --rm -p 4222:4222 nats -js
```

NATS listens on port `4222`. The `-js` flag enables JetStream (required for Key-Value storage).

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama

go run ./examples/nats-memory
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/nats-memory
```

## Session persistence

The key feature of this example is that memory **survives process restarts**. Try it:

**First run** — chat a bit, then exit:

```
> What's the capital of France?

Paris is the capital of France.

> Remember that my name is Alice.

Got it, Alice! I'll remember that.

> /exit

Goodbye!
```

**Second run** — resume the same session:

```bash
go run ./examples/nats-memory -session default
```

```
Session: default
Resumed: 4 message(s) restored from NATS

> What's my name?

Your name is Alice!
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-session` | `default` | Session ID — use the same value to resume a prior conversation |
| `-nats-url` | `""` | NATS server URL (overrides `NATS_URL` env var) |

## Environment variables

| Variable | Description |
|----------|-------------|
| `NATS_URL` | NATS server URL (default: `nats://localhost:4222`) |
| `OPENAI_API_KEY` | OpenAI API key |
| `OPENAI_BASE_URL` | Base URL for OpenAI-compatible endpoints (e.g. Ollama) |
| `OPENAI_MODEL` | Model name |

## Commands

| Command | Description |
|---------|-------------|
| `/history` | Show all stored messages |
| `/clear` | Delete all messages for this session |
| `/stats` | Show message count by role |
| `/help` | Show available commands |
| `/exit` | Exit the program |

## How it works

```go
nc, _ := nats.Connect(nats.DefaultURL)
js, _ := nc.JetStream()
kv, _ := js.CreateKeyValue(&nats.KeyValueConfig{Bucket: "phero_memory"})

mem, _ := natsmemory.New(kv, "session-123")
a.SetMemory(mem)
```

Messages are stored as a JSON-encoded `[]llm.Message` under the session key inside the `phero_memory` JetStream KV bucket. Because JetStream persists bucket data to disk, memory survives process restarts automatically.

## Multiple sessions

Run two terminals with different session IDs to have independent conversations in the same NATS bucket:

```bash
# Terminal 1
go run ./examples/nats-memory -session alice

# Terminal 2
go run ./examples/nats-memory -session bob
```

## Next steps

- [Conversational Agent](../conversational-agent/) — same REPL pattern with in-memory storage
- [Long-Term Memory](../long-term-memory/) — semantic (RAG) memory backed by Qdrant
