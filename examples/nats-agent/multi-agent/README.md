# Multi-Agent NATS Newsroom

A production-style example of three specialised agents each running as an independent NATS micro service, coordinated by a local orchestrator agent.

```
┌─────────────────────────────────────────────────────────────────┐
│                         orchestrator                            │
│  (local phero agent with three remote tools via NATS protocol) │
└────────────┬──────────────────┬──────────────────┬─────────────┘
             │ NATS             │ NATS             │ NATS
  agents.prompt.phero.newsroom.researcher/writer/editor
             │                  │                  │
   ┌─────────▼──────┐ ┌─────────▼──────┐ ┌────────▼───────┐
   │   researcher   │ │     writer     │ │     editor     │
   │  (NATS micro)  │ │  (NATS micro)  │ │  (NATS micro)  │
   └────────────────┘ └────────────────┘ └────────────────┘
```

## Pipeline

1. **researcher** — takes a topic, returns structured research notes (key facts, context, open questions)
2. **writer** — takes the research notes, produces a 300–500 word draft article
3. **editor** — takes the draft, returns a polished final article

The orchestrator discovers all three agents via the NATS Agent Protocol v0.3 service discovery (`$SRV.INFO.agents`), wraps each as an `llm.Tool` via `nats.Client.AsTool()`, and instructs the LLM to call them in order.

## Features demonstrated

| Feature | Where |
|---------|-------|
| `nats.New(nc, agent, owner, name)` — register agent as NATS micro service | researcher, writer, editor |
| `nats.NewClient(nc)` — connect to the NATS bus | orchestrator |
| `client.Discover(ctx, FilterByOwner, FilterByName)` — agent discovery | orchestrator |
| `client.AsTool(info, name, desc)` — wrap remote agent as `llm.Tool` | orchestrator |
| `WithHeartbeatInterval(...)` | all three servers |
| `WithDiscoveryTimeout(...)` / `WithInactivityTimeout(...)` | orchestrator |
| `agent.AddTool(...)` / `agent.Run(...)` — multi-step pipeline | orchestrator |
| NATS Agent Protocol v0.3 — streaming chunked responses | all servers |
| Ollama / OpenAI auto-detection via env vars | all programs |

## Prerequisites

- Go 1.25+
- A running NATS server (plain core NATS, no JetStream needed)
- An OpenAI-compatible API key **or** a locally running Ollama instance

## Start NATS

```bash
docker run --rm -p 4222:4222 nats
```

## Running

Open four terminal windows from the repository root.

**Terminal 1 — researcher agent**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/nats-agent/multi-agent/researcher
```

**Terminal 2 — writer agent**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/nats-agent/multi-agent/writer
```

**Terminal 3 — editor agent**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/nats-agent/multi-agent/editor
```

**Terminal 4 — orchestrator**
```bash
OPENAI_API_KEY=<your-key> go run ./examples/nats-agent/multi-agent/orchestrator -topic "quantum computing"
```

The orchestrator prints the discovered agent info for each server before starting, then streams the LLM's tool calls to stderr and prints the final article to stdout.

### Using Ollama (no API key)

Leave `OPENAI_API_KEY` unset and ensure Ollama is running locally. The example auto-detects Ollama and uses it as the backend.

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=llama3.2
```

### Custom model or base URL

```bash
OPENAI_MODEL=gpt-4o OPENAI_API_KEY=<your-key> go run ./examples/nats-agent/multi-agent/orchestrator
```

### Custom NATS URL

All four programs accept a `-nats-url` flag (servers via the `NATS_URL` env var):

```bash
NATS_URL=nats://myhost:4222 go run ./examples/nats-agent/multi-agent/researcher
go run ./examples/nats-agent/multi-agent/orchestrator -nats-url nats://myhost:4222 -topic "space exploration"
```

## How it works

Each agent server registers as a NATS micro service named `agents` and exposes two endpoints:

| Endpoint | Subject |
|----------|---------|
| `prompt` | `agents.prompt.phero.newsroom.<name>` |
| `status` | `agents.status.phero.newsroom.<name>` |

The orchestrator uses `$SRV.INFO.agents` fan-out discovery to find all compliant agents on the bus, filters by `owner=newsroom` and the specific instance name, then calls `client.AsTool()` to build an `llm.Tool` for each. The tool sends a JSON-encoded prompt to the agent's prompt subject and collects the streamed response chunks.

## Comparison with the A2A version

| Aspect | A2A (`examples/a2a/multi-agent`) | NATS (`examples/nats-agent/multi-agent`) |
|--------|----------------------------------|------------------------------------------|
| Transport | HTTP / JSON-RPC / SSE | NATS pub/sub |
| Discovery | Agent Card at `/.well-known/agent-card.json` | `$SRV.INFO.agents` fan-out |
| Streaming | Server-Sent Events | Chunked NATS messages |
| Infrastructure | None (plain HTTP) | NATS server |
| Interoperability | A2A SDK ecosystem | NATS Agent Protocol ecosystem |
