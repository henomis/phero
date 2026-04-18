# Parallel research (fan-out / fan-in, multi-agent example)

This example demonstrates the **parallelization / sectioning** pattern, as described in Anthropic's
[Building effective agents](https://www.anthropic.com/engineering/building-effective-agents) guide.

Multiple specialist agents investigate the same topic from independent angles concurrently (fan-out).
A Synthesizer agent then merges all findings into a single coherent report (fan-in).

Architecture:

```
                   ┌─► Historical Agent  ─────┐
                   │                           │
topic ──► fan-out ─┼─► Technical Agent   ──────┼─► Synthesizer ──► report
                   │                           │
                   └─► Societal Impact Agent ──┘
                    (all run concurrently via goroutines)
```

Key properties:
- **No shared memory** — workers are independent, preventing context cross-contamination.
- **Go-level concurrency** — fan-out is implemented with goroutines and `sync.WaitGroup`; no new framework primitives required.
- **Structured fan-in** — the synthesizer receives all findings in a single prompt, ordered by angle.

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud

go run ./examples/parallel-research
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/parallel-research -topic "nuclear fusion"
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-topic` | `"renewable energy"` | Topic to research from multiple angles |
| `-timeout` | `6m` | Overall run timeout |
