# Coding agent

A small terminal coding agent built on phero. It reads, edits and tests the code in a workspace.
It is a starting point, not a product: under 500 lines in one `main.go`.

What it uses:

- **`tool/bash`** in a **bubblewrap sandbox** (Linux): only the system directories, the workspace and the
  Go toolchain and caches are mounted, there is no network, and secrets such as `OPENAI_API_KEY` are not in
  the commands' environment. A persistent shell keeps `cd` and `export` between commands. Output is shown,
  dimmed, while a command runs (`WithOutputHandler`). Background shells are killed on exit (`Close`).
- **`tool/file`**: read, write, edit, multi_edit, glob and grep, confined to the workspace. A shared
  `ReadTracker` makes edit and write refuse a file the model has not read, or that changed since.
- **`tool/permission`**: reads, searches and a few safe commands (`go test`, `go build`, `git status`, ...)
  run without asking. Everything else asks in the terminal: `y` once, `a` always, `n` no.
- **`tool/todo`**: the agent plans multi-step tasks. The list is printed on every change and saved to
  `.phero/todo.json`.
- **`agent`**: `RunStream` for live text, summary compaction at 80% of the context window, a checkpoint
  after every tool round, and no iteration limit: a deadline per turn bounds it instead.
- **`memory/jsonfile`**: the session is saved to `.phero/session.json`, so a restart picks up the
  conversation.

The system prompt includes `AGENTS.md` (or `CLAUDE.md`) from the workspace when there is one.

## Run

Linux with [bubblewrap](https://github.com/containers/bubblewrap) installed (`apt install bubblewrap`):

```bash
export OPENAI_API_KEY=...your_key...
export OPENAI_MODEL=gpt-4o-mini

go run ./examples/coding-agent -dir /path/to/your/repo
```

Any OpenAI-compatible server works, for example a local llama.cpp:

```bash
OPENAI_BASE_URL=http://localhost:8081/v1 OPENAI_MODEL=my-model \
  go run ./examples/coding-agent -dir /path/to/your/repo
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-dir` | `.` | workspace the agent works in; its state goes in `<dir>/.phero` |
| `-context` | `100000` | the model's context window in tokens; compaction starts at 80% |
| `-turn-timeout` | `30m` | deadline of one turn |
| `-unsafe-local` | `false` | without bubblewrap, run commands on the host, **unsandboxed** |

Type a request at the `>` prompt. Ctrl-C stops the current turn; at the prompt it quits, as `/exit` does.
Delete `.phero/` to start a fresh session.

## What it is not

There is no diff view, undo, git integration, slash commands, model switching or steering while a turn
runs. Those are the app's job. Where phero has a hook for them:

- **diff / undo**: a tool middleware (`tool.Use`) on write, edit and multi_edit can record each file
  before the change.
- **model switching or a new system prompt**: build a new agent with `agent.New` and give it the same
  memory (`SetMemory`). The next run continues the session.
- **steering**: stop the turn with Ctrl-C and send the correction as the next request. The session keeps
  what was done.
- **a cut-off answer**: the agent prints a note when the model hit its output limit
  (`Result.FinishReason == llm.FinishLength`). Ask it to continue.

## Safety

Bash permission rules are advisory: they match the command text, so they stop mistakes, not a determined
model. The sandbox is the boundary. See "Running a coding agent safely" in the tool docs
(`web/docs/tool.html#coding-agent-safety`).
