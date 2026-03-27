# Changelog

## v0.0.2 - 2026-03-27

### Added

- Anthropic LLM backend under `llm/anthropic`.
- New tracing package with typed events, noop/text tracers, and a dedicated tracing example.
- New memory backends for JSON file storage and PostgreSQL storage.
- New bash tool and expanded file tooling with `view`, `create_file`, and `str_replace` operations.
- Internal SQL utilities and broader automated test coverage across core packages.

### Improved

- Agent and LLM ergonomics, including tool validation, session persistence error surfacing, and provider-side error handling.
- Skill support and tool middleware integration.
- RAG, memory, and vector store reliability around collection management, transient failures, summary handling, and payload validation.
- File tool path safety, symlink escape checks, and file size controls.
- Human-in-the-loop tool configurability with custom input and output writers.

### Documentation and Examples

- README refreshed to cover the current architecture and feature set.
- Examples updated across conversational, long-term memory, multi-agent, skills, MCP, and supervisor workflows.
- Web documentation extended, including new tracing documentation pages.

## v0.0.1

### Added

- Initial public release of the Phero framework.
- Core agent orchestration, LLM abstractions, tools, skills, MCP, RAG, memory, embeddings, and vector store foundations.
- Initial examples and project documentation.