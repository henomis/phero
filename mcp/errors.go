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

package mcp

import "fmt"

// ToolResultError is returned by an MCP tool whose call result has IsError
// set: the server ran the tool and the tool failed. Message holds the
// result's text, which usually explains the failure.
type ToolResultError struct {
	Name    string
	Message string
}

func (e *ToolResultError) Error() string {
	return fmt.Sprintf("mcp tool %q failed: %s", e.Name, e.Message)
}
