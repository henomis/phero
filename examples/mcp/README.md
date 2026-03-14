# MCP example

This example shows how to:

- run an MCP server as a subprocess over **stdio**
- discover its tools via MCP
- expose those tools to an `agent.Agent`

The bundled server provides one tool: `get_random_quote`, backed by the public https://zenquotes.io API.

## Run

### 1) Build the MCP server

From the repo root:

```bash
make -C ./examples/mcp/server build
```

This produces the binary at `./examples/mcp/server/server`.

### 2) Configure the LLM

The client uses these environment variables:

- `OPENAI_API_KEY` (optional for local OpenAI-compatible servers)
- `OPENAI_BASE_URL` (optional; if unset and no key is provided, it defaults to an Ollama-compatible base URL)
- `OPENAI_MODEL` (optional)

#### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama
```

#### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

# optional
# export OPENAI_BASE_URL=https://api.openai.com/v1
```

### 3) Run the client

Important: run from the **repo root**, because the client launches the server using a relative path (`./examples/mcp/server/server`).

```bash
go run ./examples/mcp
```

You should see something like:

```text
LLM used: model=... base_url=...
Agent response: ...
```

## What it does

- Starts `./examples/mcp/server/server` as a subprocess.
- Connects via MCP using a command (stdio) transport.
- Converts the server’s exposed MCP tools into agent tools.
- Asks the agent: "Give me a random quote." (the agent can call `get_random_quote`).

## Notes / troubleshooting

- The MCP server makes an outbound HTTP request to `https://zenquotes.io/api/random`.
- If you run the client from a different working directory, it will fail to find the server binary; re-run from the repo root or adjust the command path in `examples/mcp/main.go`.
