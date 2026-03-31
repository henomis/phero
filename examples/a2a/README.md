# A2A examples

This folder contains two examples that demonstrate Phero's A2A integration:

- `server/`: exposes a local Phero agent as an A2A server over HTTP
- `client/`: connects to that server, wraps the remote agent as an `llm.Tool`, and uses it from another Phero agent

Together they show both sides of the integration:

1. publish a Phero agent as an A2A endpoint
2. discover that endpoint from another process
3. turn the remote A2A agent into a local tool

## Layout

- [server/main.go](server/main.go): starts an HTTP server, publishes the Agent Card, and handles A2A JSON-RPC requests
- [client/main.go](client/main.go): resolves the remote Agent Card and calls the remote agent through `a2a.Client.AsTool()`

## Run

Run the server first, then the client.

### Option A: Local Ollama-compatible server

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty

go run ./examples/a2a/server
```

In a second terminal:

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud

go run ./examples/a2a/client
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/a2a/server
```

In a second terminal:

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/a2a/client
```

## What the server exposes

The server example mounts two handlers explicitly:

- `/.well-known/agent-card.json`: public Agent Card
- `/`: A2A JSON-RPC endpoint

The example binds to `:8080`, so the client connects to `http://localhost:8080`.

## Expected output

Server:

```text
A2A server listening on :8080
LLM: model=... base_url=...
AgentCard: http://localhost:8080/.well-known/agent-card.json

# After the client sends a request, the server emits trace events such as:
15:04:05.123 [greeter] ▶  AgentStart  input="Please greet Alice via the remote greeter agent."
15:04:05.124 [greeter] ↻  iteration=1
15:04:05.380 [greeter] ←  LLMResponse  iter=1 in=... out=...  tool_calls=0 content="Hello Alice! It's great to meet you."
15:04:05.381 [greeter] ■  AgentEnd  iterations=1 output="Hello Alice! It's great to meet you."
```

Client:

```text
LLM: model=... base_url=...
15:04:05.120 [orchestrator] ▶  AgentStart  input="Please greet Alice via the remote greeter agent."
15:04:05.121 [orchestrator] ↻  iteration=1
15:04:05.300 [orchestrator] ⚙  ToolCall  iter=1 tool=greeter args={"input":"Please greet Alice via the remote greeter agent."}
15:04:05.382 [orchestrator] ✓  ToolResult  iter=1 tool=greeter result={"output":"Hello Alice! It's great to meet you."}
15:04:05.540 [orchestrator] ■  AgentEnd  iterations=2 output="Hello Alice! It's great to meet you."
Hello Alice! It's great to meet you.
```

The exact greeting will vary based on the model.

## What this demonstrates

- how to expose a Phero agent through the `a2a` package
- how the caller owns the HTTP routing layer and mounts the handlers manually
- how to resolve a remote A2A Agent Card with `a2a.NewClient`
- how to convert a remote A2A agent into an `llm.Tool` with `AsTool()`

## Notes

- Both examples use `OPENAI_API_KEY`, `OPENAI_BASE_URL`, and `OPENAI_MODEL` through a local `buildLLMFromEnv()` helper.
- If both `OPENAI_API_KEY` and `OPENAI_BASE_URL` are empty, the examples default to an Ollama-compatible base URL.
- The client assumes the server is reachable at `http://localhost:8080`.
- Both agents use the built-in `trace/text` tracer, which writes human-readable trace lines to stderr.