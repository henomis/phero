# Orchestrator-Workers (dynamic decomposition, multi-agent example)

This example demonstrates the **orchestrator-workers** pattern, as described in Anthropic's
[Building effective agents](https://www.anthropic.com/engineering/building-effective-agents) guide.

An Orchestrator agent receives a high-level goal and **dynamically decides** which worker agents to call
and in what order. The task decomposition is LLM-driven — no fixed workflow is hardcoded in Go.

Architecture:

```
goal
  │
  ▼
Orchestrator (decides decomposition at runtime)
  │
  ├─► research("what are recent breakthroughs in quantum computing?")
  │         └──► Researcher worker ──► findings
  │
  ├─► write("write an intro based on: <findings>")
  │         └──► Writer worker ──► draft section
  │
  ├─► research("who are the key players in the quantum computing market?")
  │         └──► Researcher worker ──► findings
  │
  ├─► write("write a business landscape section based on: <findings>")
  │         └──► Writer worker ──► draft section
  │
  └─► critique("review and improve this full draft: <combined draft>")
            └──► Critic worker ──► final report
```

Key distinction from `supervisor-blackboard`:
- In `supervisor-blackboard` the workflow sequence (research → draft → critique) is **baked into the orchestrator's system prompt**.
- Here the orchestrator is told only *what workers exist*, not *how to use them* — it figures out the steps from the goal at runtime.

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud

go run ./examples/orchestrator-workers
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/orchestrator-workers \
  -goal "Write a briefing on the risks and opportunities of AI regulation in the EU."
```

### Flags

| Flag | Default | Description |
|---|---|---|
| `-goal` | `"Produce a comprehensive briefing on quantum computing..."` | High-level goal for the orchestrator |
| `-timeout` | `8m` | Overall run timeout |

