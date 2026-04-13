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

package main

import (
	"context"
	"fmt"

	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type EchoInput struct {
	Message string `json:"message"`
}

type EchoOutput struct {
	Output string `json:"output"`
}

func main() {
	server := gomcp.NewServer(&gomcp.Implementation{Name: "e2e-echo", Version: "1.0.0"}, nil)
	gomcp.AddTool(server, &gomcp.Tool{Name: "echo", Description: "Echoes the provided message"}, echoHandler)
	if err := server.Run(context.Background(), &gomcp.StdioTransport{}); err != nil {
		panic(err)
	}
}

func echoHandler(_ context.Context, _ *gomcp.CallToolRequest, in *EchoInput) (*gomcp.CallToolResult, EchoOutput, error) {
	message := ""
	if in != nil {
		message = in.Message
	}
	output := EchoOutput{Output: fmt.Sprintf("echo:%s", message)}
	return nil, output, nil
}
