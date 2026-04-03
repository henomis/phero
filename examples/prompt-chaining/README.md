# Prompt chaining with programmatic gate (multi-agent example)

This example demonstrates the **prompt chaining** workflow pattern, as described in Anthropic's
[Building effective agents](https://www.anthropic.com/engineering/building-effective-agents) guide.

Three agents are wired in sequence. Between step 1 and step 2, a **programmatic Go gate** validates the
output before passing it forward — intentionally illustrating the distinction between a *workflow*
(deterministic, code-controlled) and an *agent* (LLM-controlled).

Architecture:

```
topic
  │
  ▼
Outliner ──► JSON outline ──► [Go gate: ≥2 sections, ≥2 key points each]
                                        │
                                        ▼
                                    Expander ──► prose draft
                                                      │
                                                      ▼
                                                 Formatter ──► polished article
```

Key properties:
- **Structured intermediate output** — the Outliner returns JSON that Go can validate and inspect.
- **Programmatic gate** — `gateOutline()` is pure Go; it rejects malformed outlines before they reach the Expander. This is cheaper and more deterministic than using an LLM to validate structure.
- **Clear agent responsibilities** — each agent has one narrowly scoped task; no agent knows about the others.

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud

go run ./examples/prompt-chaining
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/prompt-chaining -topic "The future of space exploration"
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-topic` | `"The impact of artificial intelligence on software development"` | Document topic |
| `-timeout` | `5m` | Overall run timeout |
