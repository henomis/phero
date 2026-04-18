![Phero](./web/images/phero-logo.png)

# 🐜 Phero 

**The chemical language of AI agents.**

Phero is a modern Go framework for building multi-agent AI systems. Like ants in a colony, agents in Phero cooperate, communicate, and coordinate toward shared goals, each with specialized roles, working together through a clean, composable architecture.

[![Build Status](https://github.com/henomis/phero/actions/workflows/checks.yml/badge.svg)](https://github.com/henomis/phero/actions/workflows/checks.yml) [![GoDoc](https://godoc.org/github.com/henomis/phero?status.svg)](https://godoc.org/github.com/henomis/phero) [![Go Report Card](https://goreportcard.com/badge/github.com/henomis/phero)](https://goreportcard.com/report/github.com/henomis/phero) [![GitHub release](https://img.shields.io/github/release/henomis/phero.svg)](https://github.com/henomis/phero/releases)



## Why Phero?

- **🤝 Agent orchestration** Multi-agent workflows with role specialization, coordination, and agent handoffs
- **🧩 Composable primitives** Small, focused packages that solve specific problems
- **🔧 Tool-first design** Built-in support for function tools, skills, RAG, and MCP
- **🎨 Developer-friendly** Clean APIs, opt-in tracing, OpenAI-compatible + Anthropic support
- **🪶 Lightweight** No heavy dependencies; just Go and your choice of LLM provider



## Features

### Core Capabilities

- **🤝 Agent orchestration** Multi-agent workflows with role specialization, coordination, and runtime handoffs
- **🔀 Agent handoffs** Transfer control between agents at runtime; `Result.HandoffAgent` tells you where to route next
- **🌐 A2A protocol** Expose any agent as an HTTP A2A server, or call remote A2A agents as local tools
- **🧩 LLM abstraction** Work with OpenAI-compatible endpoints (OpenAI, Ollama, etc.) and Anthropic
- **🖼️ Multimodal input** Mix text and images with typed content parts (`llm.Text`, `llm.ImageURL`, `llm.ImageFile`)
- **🔊 Audio I/O** OpenAI backend supports speech-to-text and text-to-speech via `llm.Transcriber` and `llm.SpeechSynthesizer`
- **🧱 LLM middleware** Compose reusable cross-cutting behaviors around any backend with `llm.Use(...)`
- **🛠️ Function tools** Expose Go functions as callable tools with automatic JSON Schema generation
- **📚 RAG (Retrieval-Augmented Generation)** Built-in vector storage and semantic search
- **🧠 Skills system** Define reusable agent capabilities in `SKILL.md` files
- **🔌 MCP support** Integrate Model Context Protocol servers as agent tools
- **🧾 Memory management** Conversational context storage for agents
- **🔍 Tracing** Typed lifecycle events with a colorized text tracer (`trace/text`) and an NDJSON file tracer (`trace/jsonfile`); per-run summary with token usage and latency breakdowns
- **🛡️ Tool guardrails** Bash tool blocklist, allowlist, timeout, and safe-mode options
- **✂️ Text splitting** Recursive and Markdown-aware chunkers under `textsplitter/recursive` and `textsplitter/markdown`
- **🧬 Embeddings** Semantic search capabilities via OpenAI embeddings
- **🗄️ Vector stores** Qdrant, PostgreSQL/pgvector, and Weaviate backends



### Requirements

- Go 1.25.5 or later
- An LLM provider (OpenAI / Ollama / OpenAI-compatible endpoint, or Anthropic)



## Quick Start

Start with the **[Simple Agent](examples/simple-agent/)** example to learn the basics in ~100 lines of code.

Then try:
- **[Conversational Agent](examples/conversational-agent/)** a multi-turn REPL chatbot with short-term memory
- **[Long-Term Memory](examples/long-term-memory/)** semantic (RAG) memory backed by Qdrant

Then explore the **[examples/](examples/)** directory for more advanced patterns:
- Multi-agent workflows
- Multimodal and audio pipelines
- RAG chatbots
- Skills integration
- MCP server connections

Some examples require extra services (e.g. Qdrant for vector search).



## Architecture

Phero is organized into focused packages, each solving a specific problem:

### 🤖 Agent Layer

- **`agent`** Core orchestration for LLM-based agents with tool execution, chat loops, and runtime handoffs
- **`memory`** Conversational context management for multi-turn interactions (in-process, file-backed, RAG-backed, or PostgreSQL-backed)

### 💬 LLM Layer

- **`llm`** Provider-agnostic chat interface with typed messages/content parts, function tools, JSON Schema utilities, audio interfaces, and LLM middleware composition
- **`llm/openai`** OpenAI-compatible client (works with OpenAI, Ollama, and compatible endpoints)
- **`llm/anthropic`** Anthropic API client

### 🧠 Knowledge Layer

- **`embedding`** Embedding interface for semantic operations
- **`embedding/openai`** OpenAI embeddings implementation
- **`vectorstore`** Vector storage interface for similarity search
- **`vectorstore/qdrant`** Qdrant vector database integration
- **`vectorstore/psql`** PostgreSQL + pgvector integration
- **`vectorstore/weaviate`** Weaviate vector database integration
- **`textsplitter`** Text splitting interface and shared types
- **`textsplitter/recursive`** Recursive character-based chunker
- **`textsplitter/markdown`** Markdown-aware chunker (heading-first separators)
- **`rag`** Complete RAG pipeline combining embeddings and vector stores

### 🔧 Tools & Integration

- **`skill`** Parse SKILL.md files and expose them as agent capabilities
- **`mcp`** Model Context Protocol adapter for external tool integration
- **`a2a`** Agent-to-Agent (A2A) protocol — expose agents as HTTP servers or call remote agents as tools
- **`trace`** Typed observability events; `trace/text` for human-readable colorized output; `trace/jsonfile` for NDJSON file logging; `trace.NewLLM` for raw LLM call wrapping
- **`tool/file`** File viewing and editing helpers (`view`, `create_file` with optional no-overwrite, `str_replace`)
- **`tool/bash`** Bash command execution with blocklist, allowlist, timeout, and safe-mode guardrails
- **`tool/human`** Human-in-the-loop input collection



## Examples

Comprehensive examples are included in the [`examples/`](examples/) directory:

| Example | Description |
|---|---|
| [Simple Agent](examples/simple-agent/) | **Start here!** Minimal example showing one agent with one custom tool perfect for learning the basics |
| [Multimodal](examples/multimodal/) | Send text + image inputs to a vision-capable model using typed content parts |
| [Audio](examples/audio/) | End-to-end speech-to-text and text-to-speech using the OpenAI backend |
| [LLM Middleware](examples/llm-middleware/) | Wrap an LLM with composable middleware for logging and other cross-cutting concerns |
| [Conversational Agent](examples/conversational-agent/) | REPL-style chatbot with short-term conversational memory and a simple built-in tool |
| [Long-Term Memory](examples/long-term-memory/) | REPL-style chatbot with semantic long-term memory (RAG) backed by Qdrant |
| [Handoff](examples/handoff/) | One agent hands work off to a specialist agent at runtime using the built-in handoff mechanism |
| [A2A Server](examples/a2a/server/) | Expose a Phero agent as an A2A-compliant HTTP server for cross-process agent calls |
| [A2A Client](examples/a2a/client/) | Connect to a remote A2A agent and use it as a local tool inside an orchestrator |
| [Debate Committee](examples/debate-committee/) | Multi-agent architecture where committee members debate independently and a judge synthesizes the final decision |
| [Evaluator-Optimizer](examples/evaluator-optimizer/) | Iterative generation loop where an optimizer proposes drafts and an evaluator critiques them until quality criteria are met |
| [Human-in-the-Loop](examples/human-in-the-loop/) | Multi-agent flow that pauses for explicit human approval/input before continuing |
| [Multi-Agent Workflow](examples/multi-agent-workflow/) | Classic Plan → Execute → Analyze → Critique pattern with specialized agent roles |
| [Orchestrator-Workers](examples/orchestrator-workers/) | Dynamic task decomposition where an orchestrator delegates sub-tasks to worker agents |
| [Parallel Research](examples/parallel-research/) | Fan-out/fan-in workflow that runs multiple specialist researchers in parallel and merges their findings |
| [Prompt Chaining](examples/prompt-chaining/) | Sequential multi-step prompting with a programmatic gate between stages |
| [RAG Chatbot](examples/rag-chatbot/) | Terminal chatbot with semantic search over local documents using Qdrant |
| [Skill](examples/skills/) | Discover SKILL.md files and expose them as callable agent tools |
| [Social Simulation](examples/social-simulation/) | Multi-agent social simulation with persona-driven actors and emergent interactions |
| [MCP Integration](examples/mcp/) | Run an MCP server as a subprocess and expose its tools to agents |
| [Playwright MCP](examples/playwright-mcp/) | Connect browser automation tools through MCP and orchestrate them from an agent |
| [Supervisor Blackboard](examples/supervisor-blackboard/) | Supervisor-worker pattern with a shared blackboard for coordination |
| [Tracing](examples/tracing/) | Attach a colorized tracer to an agent and inspect LLM requests, tool calls, and memory events in real time |



## Design Philosophy

Phero embraces several core principles:

1. **Composability over monoliths** Each package does one thing well
2. **Interfaces over implementations** Swap LLMs, vector stores, or embeddings easily
3. **Explicit over implicit** No hidden magic; clear control flow
4. **Tools are first-class** Function tools are the primary integration point
5. **Developer experience matters** Clean APIs, helpful tracing, good error messages




## Contributing

Contributions are welcome! Please feel free to submit issues, feature requests, or pull requests.



## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.



## Acknowledgments

Built with ❤️ by [Simone Vellei](https://github.com/henomis).

Inspired by the collaborative intelligence of ant colonies where independent agents work together toward shared goals, recognizing one another and coordinating through clear protocols.

**The ant is not just a mascot. It is the philosophy.** 🐜



## Links

- **Documentation**: [pkg.go.dev/github.com/henomis/phero](https://pkg.go.dev/github.com/henomis/phero)
- **Issues**: [github.com/henomis/phero/issues](https://github.com/henomis/phero/issues)
- **Discussions**: [github.com/henomis/phero/discussions](https://github.com/henomis/phero/discussions)
