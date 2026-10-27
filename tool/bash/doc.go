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
// This tool executes arbitrary shell commands supplied by the language model.
// With the default LocalRunner they run with the full privileges of the
// running process and inherit its whole environment, secrets included.
// WithBlocklist and WithAllowlist match substrings of the command and are
// easy to get around: they are not a security boundary.
//
// Before using this tool in any agent, consider the following precautions:
//
//   - Run commands in a sandbox: WithRunner(NewBubblewrapRunner(workspace))
//     on Linux mounts only the system directories (read-only) and the
//     workspace, so host sockets and files such as ~/.ssh are out of reach,
//     cuts the network (WithNetwork(true) shares the host's, localhost
//     services included) and limits the environment. For stronger isolation, write a
//     Runner that runs commands in a container or a VM.
//   - Limit the environment with WithEnvAllowlist(DefaultEnvAllowlist()...)
//     so API keys and tokens do not reach the commands.
//   - Run the agent process under a dedicated low-privilege OS user.
//   - Restrict the working directory via WithWorkingDirectory.
//   - Apply a ToolMiddleware that asks a human before running commands it
//     does not recognize.
//   - Never expose this tool to untrusted or adversarial prompt inputs without
//     human-in-the-loop approval.
//
// Failure to apply appropriate mitigations may allow an LLM to read, write, or
// delete arbitrary files, exfiltrate secrets, or execute network requests.
package bash
