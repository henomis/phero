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
  ├─► ask_human("I plan to create a Dockerfile. Approve?")
  │         │
  │    human response
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
- **`tool/human`** — the built-in `ask_human` tool presents a question on stdout and reads the answer from stdin.
- **Agent-controlled gate** — the agent itself decides when to ask; it is instructed never to simulate an action without prior approval.
- **No hardcoded workflow** — the agent plans its own steps based on the goal; the human controls what actually runs.
- **Interactive by design** — this example requires stdin interaction and is not suitable for CI.

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

When prompted, respond with:
- `yes` / `ok` / `proceed` — approve the action
- `no` / `skip` — skip this action
- `stop` / `abort` — stop all remaining actions
- Any freeform text — the agent will adjust accordingly

### Flags

| Flag | Default | Description |
|---|---|---|
| `-goal` | `"Set up a new Go microservice project..."` | High-level goal for the agent |
| `-timeout` | `10m` | Overall run timeout |
