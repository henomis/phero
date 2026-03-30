# Handoff Chatbot

Demonstrates **agent handoff** in an interactive chatbot — every user message is first routed through a triage agent which transfers control to the right specialist, **preserving full conversation context** through a shared memory instance.

This example shows:

- **Agent handoff** — `AddHandoff` registers a specialist as a callable tool
- **Shared memory** — a single `simple.Memory` is given to every agent so context survives across handoffs and turns
- **Chatbot loop** — each user message re-enters the triage agent, which routes to the appropriate specialist

## Architecture

```
User message
    │
    ▼
Triage Agent  ──handoff──►  Billing Agent
             ──handoff──►  Technical Support Agent
```

All three agents share the **same** `simple.Memory` instance.  
When the triage agent hands off, the user's message is already in shared memory.  
The specialist retrieves that context and replies with full awareness of the original request.  
On the next turn, the cycle restarts from the triage agent.

## What you'll learn

- How to call `agent.AddHandoff` to register a target agent as a routing tool
- How to drive the handoff loop in application code (`result.HandoffAgent`)
- Why shared memory is required for the specialist to have context
- How to build a multi-turn chatbot on top of the handoff pattern

## Run

### Option A: Local Ollama (OpenAI-compatible)

```bash
export OPENAI_BASE_URL=http://localhost:11434/v1
export OPENAI_MODEL=gpt-oss:20b-cloud

go run ./examples/handoff
```

### Option B: OpenAI

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/handoff
```

## Chat commands

| Command  | Action                          |
|----------|---------------------------------|
| `/clear` | Clear the shared conversation memory |
| `/exit`  | Quit the chatbot                |

## Example output

```
Handoff Chatbot
───────────────────────────────────────
LLM: model=gpt-4o-mini

Commands: /clear  /exit
Every message is triaged and routed to the right specialist.
───────────────────────────────────────

> I was charged twice for my subscription last month.
[handoff] Triage Agent → Billing Agent

Billing Agent: I'm sorry to hear you were charged twice! I can see a duplicate charge
on your last billing cycle. I'll process a full refund within 3–5 business days.
Is there anything else I can help with?

> The /upload endpoint keeps returning 503 errors.
[handoff] Triage Agent → Technical Support Agent

Technical Support Agent: A 503 on /upload usually means the service is temporarily
overloaded. Here are a few things to check: ...
```
