# Debate committee + judge (multi-agent example)

This example demonstrates a classic “committee debate → judge decision” multi-agent architecture.

Compared to the repo’s Plan→Execute→Analyze→Critique example, this pattern is useful when:

- you want multiple independent proposals (diverse perspectives)
- you want an explicit synthesis/decision step
- you want to reduce single-agent blind spots

Architecture:

1. **Committee members** (e.g. Advocate, Skeptic, Minimalist) each answer the same question independently.
2. A **Judge** agent reviews the arguments, identifies conflicts/assumptions, and produces one final answer.

This example intentionally does **not** share memory between committee members, to keep the arguments more independent.

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud
# OPENAI_API_KEY can be empty for Ollama

go run ./examples/debate-committee
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/debate-committee -question "How would you design a multi-agent workflow for safe repo triage?"
```
