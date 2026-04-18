# Social Simulation

A **MiroFish-inspired** multi-agent social simulation built entirely on the Phero SDK.

Feed a seed scenario (a news article, policy brief, or any text), and the system will:

1. Extract structured world facts from the seed
2. Generate N diverse fictional personas with conflicting stances
3. Run M simulation rounds — all agents post concurrently, reacting to each other
4. Synthesize a structured prediction report analyzing opinion dynamics
5. *(Optional)* Drop into an interactive Q&A with the report agent

## Architecture

```
seed text
    │
    ▼
KnowledgeExtractor  ──► world facts
                              │
                              ▼
                     PersonaOrchestrator ──► N Persona agents (each with memory)
                                                      │
                                          ┌───────────┴───────────┐
                                          │    Simulation rounds   │
                                          │  (goroutine fan-out)  │
                                          │  Round 1 → WorldFeed  │
                                          │  Round 2 → WorldFeed  │
                                          │  ...                   │
                                          └───────────┬───────────┘
                                                      │
                                                      ▼
                                               ReportAgent
                                                      │
                                              (interactive REPL)
```

### Design tradeoffs vs MiroFish

| MiroFish feature | Phero approach | Tradeoff |
|---|---|---|
| GraphRAG (entity graph) | LLM-based extraction → flat world facts | Relationships stored as prose, not traversable |
| Zep Cloud long-term memory | `simple.Memory` per agent (bounded FIFO + summarization option) | No cross-session persistence by default |
| OASIS (1 M agents) | Goroutine fan-out, all agents concurrent per round | Practical cap ~20 agents (LLM cost/latency) |
| Dual-platform social graph | Shared `WorldFeed` transcript | No follower graph; all agents see the same feed |
| Deep agent chat | Interactive REPL with `ReportAgent` backed by memory | Chat with report analyst, not individual personas |

## Usage

```bash
# Default scenario (gas vehicle ban)
go run .

# Custom inline seed
go run . --seed "A tech company announces mandatory return-to-office for all remote workers."

# From a file
go run . --seed ./article.txt --agents 12 --rounds 8

# Enable interactive Q&A after the report
go run . --interact

# All flags
go run . \
  --seed    "your scenario here"  \
  --agents  10                    \   # number of persona agents (default: 8)
  --rounds   5                    \   # simulation rounds        (default: 5)
  --topk    15                    \   # feed entries per agent   (default: 15)
  --timeout 20m                   \   # overall timeout          (default: 15m)
  --interact                          # interactive REPL         (default: false)
```

## Environment variables

| Variable | Description |
|---|---|
| `OPENAI_API_KEY` | API key (leave empty for local Ollama) |
| `OPENAI_BASE_URL` | Base URL override (e.g. Ollama, vLLM) |
| `OPENAI_MODEL` | Model name override |

## Cost guidance

Each run makes approximately `agents × rounds + 3` LLM calls.
Default settings (8 agents × 5 rounds): ~43 calls.
Start small and scale up once you're happy with the results.

## Example output

```
social simulation
- llm: model=gpt-4o
- agents: 8  rounds: 5  topk: 15
- estimated LLM calls: ~43

phase 1/4: extracting world facts...
world facts extracted.

phase 2/4: generating 8 personas...
8 personas generated:
  - Maria Lopez (pragmatic, community-focused)
  - James Okafor (skeptical, data-driven, direct)
  ...

phase 3/4: running 5 simulation rounds...
  round 1/5
    [Maria Lopez] This policy is exactly what our city needs to…
    [James Okafor] The timeline is completely unrealistic. Banning…
    ...

=== simulation report ===

## Opinion Evolution
In round 1, opinions split sharply between environmental advocates...

## Coalitions & Dynamics
By round 3, Maria Lopez and Priya Sharma formed a pro-policy coalition...

## Key Inflection Points
Round 2: James Okafor's economic argument shifted two previously undecided agents...

## Final Outlook
The simulation suggests polarized public opinion with moderate voices...
```
