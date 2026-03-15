// Package anthropic provides an llm.LLM implementation backed by Anthropic's
// Messages API.
//
// This package is intentionally OpenAI-shaped at its boundaries because the
// core `llm` package re-exports OpenAI chat message and tool-call types.
// Internally, it converts:
//
//   - OpenAI-style messages (system/user/assistant/tool) into Anthropic Messages API params
//   - Anthropic tool_use blocks into OpenAI-style tool calls
package anthropic
