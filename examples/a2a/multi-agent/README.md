# Multi-Agent A2A Newsroom

A production-style example of three specialised agents each running as an independent A2A server, coordinated by a local orchestrator agent.

```
┌─────────────────────────────────────────────────────────┐
│                     orchestrator                        │
│  (local phero agent with three remote tools via A2A)   │
└────────────┬──────────────┬──────────────┬─────────────┘
             │ A2A          │ A2A          │ A2A
     ┌───────▼──────┐ ┌─────▼──────┐ ┌────▼───────┐
     │  researcher  │ │   writer   │ │   editor   │
     │   :8081      │ │   :8082    │ │   :8083    │
     └──────────────┘ └────────────┘ └────────────┘
```

## Pipeline

1. **researcher** — takes a topic, returns structured research notes (key facts, context, open questions)
2. **writer** — takes the research notes, produces a 300–500 word draft article
3. **editor** — takes the draft, returns a polished final article

The orchestrator agent discovers all three via their AgentCards, wraps each as an `llm.Tool`, and instructs the LLM to call them in order.

## Features demonstrated

| Feature | Where |
|---------|-------|
| `WithStreaming()` | researcher, writer, editor |
| `WithRESTTransport()` (HTTP+JSON/SSE) | researcher only |
| `WithSkills(...)` | all three servers |
| `WithProvider(...)` | all three servers |
| `WithVersion(...)` | all three servers |
| `WithCallInterceptors(...)` (method logger) | editor |
| `srv.Mount(mux)` convenience | all three servers |
| `WithAcceptedOutputModes(...)` | orchestrator clients |
| `WithPreferredTransports(...)` | orchestrator → researcher |
| `client.Card()` (print discovered metadata) | orchestrator |
| Real task cancellation via `context.CancelFunc` | executor internals |
| Async task polling (`waitForTask`) | client internals |

## Prerequisites

- Go 1.25+
- An OpenAI-compatible API key **or** a locally running Ollama instance

## Running

Open four terminal windows from the repository root.

**Terminal 1 — researcher agent**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/a2a-complex/multi-agent/researcher
```

**Terminal 2 — writer agent**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/a2a-complex/multi-agent/writer
```

**Terminal 3 — editor agent**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/a2a-complex/multi-agent/editor
```

**Terminal 4 — orchestrator**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/a2a-complex/multi-agent/orchestrator -topic "quantum computing"
```

The orchestrator prints the discovered AgentCard metadata for each server before starting, then streams the LLM's tool calls to stderr and prints the final article to stdout.

### Using Ollama (no API key)

Leave `OPENAI_API_KEY` unset and ensure Ollama is running locally. The example auto-detects Ollama and uses it as the backend.

### Custom model or base URL

```bash
OPENAI_MODEL=gpt-4o OPENAI_API_KEY=<your-key> go run ./examples/a2a-complex/multi-agent/orchestrator
```

## Agent card endpoints

Once the servers are running you can inspect their cards directly:

```bash
curl http://localhost:8081/.well-known/agent-card.json | jq .   # researcher
curl http://localhost:8082/.well-known/agent-card.json | jq .   # writer
curl http://localhost:8083/.well-known/agent-card.json | jq .   # editor
```

The researcher also exposes a REST/SSE endpoint at `http://localhost:8081/rest/`.

## Stopping

`Ctrl-C` in each terminal. Each server handles `SIGINT`/`SIGTERM` and shuts down cleanly.
