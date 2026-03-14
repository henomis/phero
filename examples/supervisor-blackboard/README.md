# Supervisor + Specialists + Blackboard (multi-agent example)

This example demonstrates a different classic multi-agent architecture than the repo’s Plan→Execute→Analyze→Critique demo.

Pattern shown here (widely used in practice):

- **Supervisor / Router** agent delegates work to specialists.
- **Specialist agents** are exposed as tools via `Agent.AsTool(...)`.
- A shared **blackboard memory** (`memory.Memory`) is injected into all agents so they can collaborate through a common context.

In this example the Supervisor coordinates a simple repo health-check:

1. **Researcher** runs safe Go commands (`go list`, `go test`) via a restricted `run_go` tool.
2. **Drafter** turns the findings into a short report.
3. **Critic** checks the report for unsupported claims and improves it.

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama

go run ./examples/supervisor-blackboard
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/supervisor-blackboard -goal "Run go tests and summarize what failed"
```
