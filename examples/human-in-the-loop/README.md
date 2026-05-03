# Human-in-the-loop (multi-agent example)

This example demonstrates the **human-in-the-loop** checkpoint pattern, where an agent pauses and asks
for explicit approval before executing each consequential action.

Architecture:

```
goal
  │
  ▼
DevOps Assistant
  │
  ├─► user_interaction({ structured approval question })
  │         │
  │    structured user answer
  │         │
  │    approved? ──yes──► simulate_action("create Dockerfile")
  │         │
  │        no ──────────► skip, move to next action
  │
  └─► ... (repeat for each planned action)
        │
        ▼
     final summary
```

Key properties:
- **`tool/human`** — the built-in `user_interaction` tool validates a structured question payload (questions, options, multi-select flags) and returns structured answers.
- **Host-provided interactor** — applications inject how answers are collected (CLI, web, IDE, etc.). In this example, a console callback renders options and reads stdin.
- **Agent-controlled gate** — the agent itself decides when to ask; it is instructed never to simulate an action without prior approval.
- **No hardcoded workflow** — the agent plans its own steps based on the goal; the human controls what actually runs.
- **Interactive by design** — this sample callback uses stdin interaction and is not suitable for CI.

## Run

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/human-in-the-loop
```

With a custom goal:

```bash
go run ./examples/human-in-the-loop \
  -goal "Set up infrastructure for a Python FastAPI service: virtual environment, Dockerfile, nginx config."
```

When prompted, select an option label or number:
- `Approve` / `1` — approve the action
- `Skip` / `2` — skip this action
- `Modify` / `3` — ask the agent to revise first
- `Stop` / `4` — stop all remaining actions

Optional free text can be provided as `other: <text>` in the same answer.

### Flags

| Flag | Default | Description |
|---|---|---|
| `-goal` | `"Set up a new Go microservice project..."` | High-level goal for the agent |
| `-timeout` | `10m` | Overall run timeout |
