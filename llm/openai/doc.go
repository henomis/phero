// Package openai provides an llm.LLM implementation backed by the OpenAI Chat
// Completions API.
//
// The client is configurable via functional options (model, base URL, streaming).
// In addition to OpenAI endpoints, the helpers also support OpenAI-compatible
// servers such as Ollama (via WithOllamaBaseURL).
package openai
