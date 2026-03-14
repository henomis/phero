---
name: get-random-quote
description: Fetches and returns a single random quote by running the Go script scripts/get_random_quote/main.go.
---

# Random Quote Retriever

## Overview

Use this skill when the user asks for a random quote (inspirational, motivational, general). This skill must retrieve the quote by executing the repository's Go script and returning its stdout.

**Keywords**: quote, random quote, inspirational quote, motivational quote, zenquotes, golang, go run, stdout

## How It Works

### Primary Action

- Always run the Go program at `scripts/get_random_quote/main.go` via the `go` tool.
- Return exactly the quote line printed by the program (trim surrounding whitespace).

### Tool Call Contract

- Call `go` with args equivalent to:
	- `go run ./scripts/get_random_quote/main.go`
- If `stdout` is empty but `stderr` contains an explanation, return a short failure message and include the `stderr`.

## Features

### Single Quote Output

- Produces one quote per request
- Preserves punctuation and attribution as printed by the script

### Robust Error Handling

- If the API is unreachable or returns an unexpected payload, surface the script's error output
- Do not fabricate quotes if the tool execution fails

## Technical Details

### Runtime Requirements

- Requires the `go` toolchain available on PATH
- Requires outbound network access to fetch a quote from the remote API

### Output Format

- Expected output is a single line in the form: `Quote - Author`