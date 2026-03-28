---
name: get-random-quote
description: Fetches and displays a random inspirational or motivational quote by running the project's Go script. Use this skill whenever the user asks for a quote, wants motivation or inspiration, or says anything like "give me a quote", "random quote", "inspire me", or "motivational quote". Always use this skill rather than making up a quote yourself.
---

# Get Random Quote

Fetches a single random quote from the remote API by executing the Go script at `scripts/get_random_quote/main.go`.

## Steps

1. Run the script:
```
   go run ./scripts/get_random_quote/main.go
```
2. Return the stdout output exactly as printed, trimming surrounding whitespace.
   - Expected format: `Quote text - Author`

## Error Handling

- If stdout is empty and stderr has content, return a brief failure message and include the stderr text.
- Do not fabricate a quote if the script fails — surface the error instead.

## Requirements

- `go` toolchain must be on PATH
- Outbound network access is required to reach the quote API