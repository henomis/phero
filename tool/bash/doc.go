// Copyright 2026 Simone Vellei
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package bash provides a Phero tool that executes bash commands on behalf of
// an LLM agent.
//
// # Security Warning
//
// This tool executes arbitrary shell commands supplied by the language model
// with the full privileges of the running process. There is no sandboxing,
// capability restriction, or command allowlist enforced by the tool itself.
//
// Before using this tool in any agent, consider the following precautions:
//
//   - Run the agent process under a dedicated low-privilege OS user.
//   - Use OS-level sandboxing (e.g. seccomp, Linux namespaces, Docker) to
//     limit what the process can access.
//   - Restrict the working directory via WithWorkingDirectory so the agent
//     cannot trivially navigate to sensitive paths.
//   - Apply a ToolMiddleware that validates or allowlists commands before
//     they reach the underlying exec call.
//   - Never expose this tool to untrusted or adversarial prompt inputs without
//     human-in-the-loop approval.
//
// Failure to apply appropriate mitigations may allow an LLM to read, write, or
// delete arbitrary files, exfiltrate secrets, or execute network requests.
package bash
