# NATS Agent Example

Registers a Phero agent on NATS and interacts with it over an interactive client, using the [NATS Agent Protocol v0.3](https://github.com/synadia-ai/nats-agent-sdk-docs/blob/main/core-protocol.md).

This example shows:

- **`server/`** — wrap any `agent.Agent` with `nats.New()` and call `Start()`. The agent registers as a NATS micro service discoverable via `$SRV.PING/INFO.agents`, streams responses back, and publishes heartbeats. Trace events (LLM calls, tokens, latency) go to stderr.
- **`client/`** — discover all compliant agents on the bus, connect to the first one, and start an interactive chat loop.
- **Wire compatibility** — Phero agents are fully interoperable with the TypeScript and Python SDKs from [synadia-agents](https://github.com/synadia-ai/synadia-agents/).

## What you'll learn

- How to use `nats.New` / `nats.NewClient` to expose and call agents over NATS
- How `nats.Client.AsTool()` lets one Phero agent call another as a tool
- That any client speaking the NATS Agent Protocol — not just Phero — can discover and prompt Phero agents

## Requirements

- A running NATS server (plain core NATS, no JetStream needed)

## Start NATS

```bash
docker run --rm -p 4222:4222 nats
```

## Run

### Terminal 1 — server

```bash
# Option A: Ollama (local, no API key required)
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud

go run ./examples/nats-agent/server -owner=alice -name=demo
```

```bash
# Option B: OpenAI
export OPENAI_API_KEY=sk-...

go run ./examples/nats-agent/server -owner=alice -name=demo
```

The server prints its prompt subject and blocks. Each incoming prompt is traced to stderr:

```
LLM:     model=gpt-4o-mini
Subject: agents.prompt.phero.alice.demo
Press Ctrl-C to stop.

[agent:nats-demo-agent] start
[llm] → 2 messages
[llm] ← 42 tokens (in=15, out=27)
[agent:nats-demo-agent] end  iterations=1 llm_calls=1 latency=1.23s
```

### Terminal 2 — client

```bash
go run ./examples/nats-agent/client
```

The client discovers all agents, connects to the first one, and opens a chat loop:

```
Discovering agents...
Found 1 agent(s):
  [1] agent=phero        owner=alice        name=demo         protocol=0.3

Connected to: phero/alice/demo
Type a message and press Enter. /exit to quit.

> What is NATS?
NATS is a lightweight, high-performance open-source messaging system...

> Why would I use it with AI agents?
NATS provides reliable message routing between distributed agents...

> /exit
Goodbye!
```

Filter discovery by owner, name, or agent identifier if multiple agents are running:

```bash
go run ./examples/nats-agent/client -owner=alice -name=demo
```

## Flags

### server

| Flag | Default | Description |
|------|---------|-------------|
| `-nats-url` | `""` | NATS server URL (overrides `NATS_URL` env var) |
| `-owner` | `demo` | Agent owner — 4th token in the subject hierarchy |
| `-name` | `default` | Instance name — 5th token in the subject hierarchy |

### client

| Flag | Default | Description |
|------|---------|-------------|
| `-nats-url` | `""` | NATS server URL (overrides `NATS_URL` env var) |
| `-agent` | `""` | Filter discovery by `metadata.agent` (e.g. `phero`) |
| `-owner` | `""` | Filter discovery by `metadata.owner` |
| `-name` | `""` | Filter discovery by instance name |

## Environment variables

| Variable | Description |
|----------|-------------|
| `NATS_URL` | NATS server URL (default: `nats://localhost:4222`) |
| `OPENAI_API_KEY` | OpenAI API key |
| `OPENAI_BASE_URL` | Base URL for OpenAI-compatible endpoints (e.g. Ollama) |
| `OPENAI_MODEL` | Model name override |

## Interoperability with the Python and TypeScript SDKs

Because Phero implements the wire protocol faithfully, any client that speaks NATS Agent Protocol v0.3 works against the Phero server — including the numbered demo scripts from the [synadia-agents Python SDK](https://github.com/synadia-ai/synadia-agents/client-sdk/python/examples/).

Start the Phero server, then run the Python demos from another terminal:

```bash
# Install the Python SDK (from the synadia-agents subtree)
cd synadia-agents/client-sdk/python
uv sync

# Discover the running Phero agent
uv run python examples/01-discover.py --url nats://127.0.0.1:4222

# Stream a single prompt
uv run python examples/02-prompt-text.py --url nats://127.0.0.1:4222 "What is 2+2?"

# Interactive chat REPL (requires `uv sync --extra examples`)
uv run python examples/06-chat.py --url nats://127.0.0.1:4222

# Watch heartbeats (Phero server publishes every 10 s)
uv run python examples/05-liveness.py --url nats://127.0.0.1:4222
```

### Which demos work out of the box

| Demo | Works against Phero? | Notes |
|------|---------------------|-------|
| `01-discover.py` | ✅ | Discovers the Phero service via `$SRV.INFO.agents` |
| `02-prompt-text.py` | ✅ | Streams a text prompt and prints the response |
| `03-prompt-attachment.py` | ⚠️ | Requires `nats.WithAttachmentsOk(true)` on the server; disabled by default |
| `04-query-reply.py` | ⚠️ | Requires the agent to emit mid-stream `query` chunks; not in this demo |
| `05-liveness.py` | ✅ | Tracks heartbeats emitted by the Phero server |
| `06-chat.py` | ✅ | Interactive REPL against any discovered agent |

The same works in reverse: `nats.NewClient` discovers and prompts agents registered by the Python or TypeScript `AgentService`.

## How it works

```go
// Server: expose any Phero agent over NATS
srv, _ := natsagent.New(nc, a, "alice", "demo",
    natsagent.WithAgentID("phero"),
    natsagent.WithHeartbeatInterval(10*time.Second),
)
srv.Start(ctx)

// Client: discover, prompt, stream
c := natsagent.NewClient(nc)
agents, _ := c.Discover(ctx)
stream, _ := agents[0].Prompt(ctx, "Hello!")
text, _ := stream.Text(ctx)

// AsTool: let a local agent call a remote one
tool, _ := c.AsTool(agents[0], "remote-agent", "Call the remote NATS agent")
localAgent.AddTool(tool)
```

## Note on conversation memory

Each `Prompt()` call is an independent request — the protocol has no built-in session state. If you want the server-side agent to remember previous turns, combine this example with `memory/nats` ([NATS Memory example](../nats-memory/)).

## Next steps

- [NATS Memory](../nats-memory/) — persist conversation history in NATS JetStream KV
- [A2A](../a2a/) — same agent-over-network pattern using the HTTP/SSE A2A protocol
- [Multi-agent workflow](../multi-agent-workflow/) — local sub-agent orchestration with handoffs
