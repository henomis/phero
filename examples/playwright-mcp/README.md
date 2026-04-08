# Playwright MCP (HTTP) example

This example shows how to connect Phero to an **already-running** Playwright MCP server over **HTTP** and expose its tools to an `agent.Agent`.

Target endpoint (hard-coded in the example):

- `http://localhost:8931/mcp`

## Prerequisites

1) Start your Playwright MCP server separately so it is reachable at `http://localhost:8931/mcp`.

For example, you can run Playwright's MCP server with:

```bash
npx @playwright/mcp@latest --port 8931
```

2) Configure the LLM (same env vars used by other examples):

- `OPENAI_API_KEY` (optional for local OpenAI-compatible servers)
- `OPENAI_BASE_URL` (optional; if unset and no key is provided, defaults to an Ollama-compatible base URL)
- `OPENAI_MODEL` (optional)

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

# optional
# export OPENAI_BASE_URL=https://api.openai.com/v1
```

## Run

From the repo root:

```bash
go run ./examples/playwright-mcp
```

You should see something like:

```text
LLM used: model=... base_url=...
Agent response: title=...
h1=...
```

## Notes / troubleshooting

- This example uses the MCP SDK **Streamable HTTP** transport (`StreamableClientTransport`).
- If your Playwright MCP server is using the older **SSE** transport instead, swap the transport to:

  - `gomcp.SSEClientTransport{Endpoint: "http://localhost:8931/mcp"}`
