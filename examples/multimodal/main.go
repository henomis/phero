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

// Multimodal example demonstrates passing image and text content parts to an agent.
//
// The example shows two approaches for providing images:
//  1. Remote image via URL (llm.ImageURL).
//  2. Local image from disk (llm.ImageFile) — the file is read, MIME type is
//     detected automatically, and the bytes are base64-encoded before being sent.
//
// Usage:
//
//	# URL-based image (default):
//	OPENAI_API_KEY=<key> go run ./examples/multimodal/
//
//	# Local file image:
//	OPENAI_API_KEY=<key> go run ./examples/multimodal/ /path/to/image.png
//
// The agent uses gpt-4o (vision-capable).
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	llmopenai "github.com/henomis/phero/llm/openai"
)

func main() {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	// Use gpt-4o which supports vision input.
	client := llmopenai.New(apiKey, llmopenai.WithModel("gpt-4o"))

	a, err := agent.New(client, "vision-agent", "You are a helpful assistant that can analyse images.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent.New: %v\n", err)
		os.Exit(1)
	}

	// Build the image part: use a local file if one was provided, otherwise fall
	// back to a well-known remote URL.
	var imagePart llm.ContentPart
	if len(os.Args) > 1 {
		imagePart, err = llm.ImageFile(os.Args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "ImageFile: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Sending local file: %s (%s)\n", os.Args[1], imagePart.MIMEType)
	} else {
		imagePart = llm.ImageURL("https://upload.wikimedia.org/wikipedia/commons/thumb/4/47/PNG_transparency_demonstration_1.png/280px-PNG_transparency_demonstration_1.png")
		fmt.Println("Sending remote image URL.")
	}

	result, err := a.Run(
		context.Background(),
		llm.Text("What is in this image? Please describe it briefly."),
		imagePart,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Run: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Agent:", result.TextContent())
}
