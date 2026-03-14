# Skills example

This example demonstrates how to:

- discover and parse local `SKILL.md` definitions
- turn each skill into a callable tool for an `agent.Agent`
- let the agent combine a skill output with a file-writing tool

It includes one skill: `get-random-quote`, which fetches a quote from https://zenquotes.io by running a small Go script.

## Run

### 1) Configure the LLM

The example uses these environment variables:

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

### 2) Run the example

Important: run from the `examples/skills` directory, because the example loads skills from a relative path (`./skills`).

```bash
cd ./examples/skills
go run .
```

During execution you’ll be prompted to approve tool actions (for example running `go ...` commands and writing files). Reply `y` to allow.

When it completes, you should see output like:

```text
LLM used: model=... base_url=...
Agent response: ...
```

and a file named `quote.html` will be created in the current directory.

## What it does

- Creates a skills parser rooted at `./skills`.
- Lists skill directories (each contains a `SKILL.md`).
- Parses each `SKILL.md` and converts it into a tool via `skill.AsTool(...)`.
- Adds an explicit file write tool (with an interactive validation prompt).
- Runs the agent with: "create a web page containing a random quote, and save the html to a file called quote.html".

## Notes / troubleshooting

- The `get-random-quote` skill runs `go run ./scripts/get_random_quote/main.go` under its skill directory.
- Requires a working Go toolchain in `PATH`.
- Requires outbound network access to reach `https://zenquotes.io/api/random`.
- If you run from the repo root, the example may not find `./skills`; `cd ./examples/skills` first.
