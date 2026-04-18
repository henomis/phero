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
	"os"
	"strings"

	"github.com/henomis/phero/llm"
	llmopenai "github.com/henomis/phero/llm/openai"
)

func main() {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "OPENAI_API_KEY is not set")
		os.Exit(1)
	}

	if len(os.Args) < 3 {
		usage()
		os.Exit(1)
	}

	client := llmopenai.New(apiKey)

	switch os.Args[1] {
	case "transcribe":
		transcriber, ok := any(client).(llm.Transcriber)
		if !ok {
			fmt.Fprintln(os.Stderr, "client does not support transcription")
			os.Exit(1)
		}

		result, err := transcriber.Transcribe(context.Background(), llm.TranscriptionRequest{
			Input: llm.AudioFile(os.Args[2]),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Transcribe: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(result.Text)

	case "speak":
		synthesizer, ok := any(client).(llm.SpeechSynthesizer)
		if !ok {
			fmt.Fprintln(os.Stderr, "client does not support speech synthesis")
			os.Exit(1)
		}

		outputPath := "speech.mp3"
		if len(os.Args) > 3 {
			outputPath = os.Args[3]
		}

		result, err := synthesizer.SynthesizeSpeech(context.Background(), llm.SpeechRequest{
			Input: os.Args[2],
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "SynthesizeSpeech: %v\n", err)
			os.Exit(1)
		}

		if err := os.WriteFile(outputPath, result.Data, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write output: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Wrote %s (%s)\n", outputPath, result.MIMEType)

	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  OPENAI_API_KEY=<key> go run ./examples/audio/ transcribe /path/to/audio.mp3")
	fmt.Fprintln(os.Stderr, "  OPENAI_API_KEY=<key> go run ./examples/audio/ speak \"hello world\" [output.mp3]")
}
