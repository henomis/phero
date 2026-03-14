# Multi-agent workflow example

This example demonstrates a typical **Plan → Execute → Analyze → Critique** multi-agent architecture using this repo’s primitives:

- `agent.Agent` (role + system prompt)
- `llm.FunctionTool` (tool execution)
- a safe `run_go` tool (only allows `go list` / `go test`)

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama

go run ./examples/multi-agent-workflow
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

# optional
# export OPENAI_BASE_URL=https://api.openai.com/v1

go run ./examples/multi-agent-workflow -goal "Run go tests and summarize failures"
```

## What it does

1. **Planner agent** returns a JSON plan with up to 3 safe `go` commands.
2. **Runner agent** executes each step via the `run_go` tool.
3. **Analyst agent** produces a grounded report from the step outputs.
4. **Critic agent** reviews the report for gaps/hallucinations and proposes improvements.
