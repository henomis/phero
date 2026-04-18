# Evaluator-Optimizer (multi-agent example)

This example demonstrates the **evaluator-optimizer** multi-agent architecture, as described in Anthropic's
[Building effective agents](https://www.anthropic.com/engineering/building-effective-agents) guide.

A Generator agent produces a draft. An Evaluator agent returns a structured score and actionable feedback.
The loop repeats until the score reaches a configurable threshold or the maximum number of attempts is hit.

Architecture:

```
topic ──► Generator ──► draft
                           │
                           ▼
                       Evaluator ──► score + feedback
                           │
              score < threshold?
              yes ──► revise prompt ──► Generator  (next iteration)
              no  ──► done
```

Key properties:
- **No tools needed** — both agents are pure language tasks.
- **Go-level control loop** — the orchestration logic lives in Go, not in any agent's tool calls.
- **Structured evaluator output** — the evaluator returns a JSON object (`{"score": N, "feedback": "..."}`), which Go parses to decide whether to continue.

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud

go run ./examples/evaluator-optimizer
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/evaluator-optimizer -topic "How does the internet work?" -threshold 8
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-topic` | `"Explain how large language models work..."` | Writing topic |
| `-threshold` | `8` | Minimum score (0-10) to accept |
| `-max-attempts` | `4` | Maximum generator-evaluator iterations |
| `-timeout` | `5m` | Overall run timeout |
