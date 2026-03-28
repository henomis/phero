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

package human

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/henomis/phero/llm"
)

// Input represents the input for the HumanTool, containing the question to ask the human.
type Input struct {
	Question string `json:"question" jsonschema:"description=The question or prompt to ask the human user"`
}

// Output represents the output from the HumanTool, containing the human's response.
type Output struct {
	Response string `json:"response"`
}

// Tool is a tool that allows the agent to ask a human for input.
type Tool struct {
	tool   *llm.Tool
	reader *bufio.Reader
	writer io.Writer
}

// Option configures a Tool created by New.
type Option func(*Tool)

// WithReader overrides the input reader used to capture the human's response.
//
// Default is os.Stdin.
func WithReader(r io.Reader) Option {
	return func(t *Tool) {
		t.reader = bufio.NewReader(r)
	}
}

// WithWriter overrides the output writer used to display prompts to the human.
//
// Default is os.Stdout.
func WithWriter(w io.Writer) Option {
	return func(t *Tool) {
		t.writer = w
	}
}

// New creates a new instance of HumanTool.
//
// This tool enables the agent to request human input during execution.
// The tool will prompt the user with the provided question and return their response.
func New(opts ...Option) (*Tool, error) {
	name := "ask_human"
	description := "use this tool to ask a human user for input, clarification, or decision. The input is the question or prompt to show the user. The output is the user's response."

	humanTool := &Tool{
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(humanTool)
		}
	}

	tool, err := llm.NewTool(
		name,
		description,
		humanTool.ask,
	)
	if err != nil {
		return nil, err
	}

	humanTool.tool = tool

	return humanTool, nil
}

// Tool returns the llm.Tool representation of the HumanTool.
func (h *Tool) Tool() *llm.Tool {
	return h.tool
}

func (h *Tool) ask(ctx context.Context, input *Input) (*Output, error) {
	_ = ctx
	if input == nil {
		return nil, ErrNilInput
	}

	_, _ = fmt.Fprintf(h.writer, "\n🤔 Human Input Required:\n%s\n\n", input.Question)
	_, _ = fmt.Fprint(h.writer, "Your response: ")

	response, err := h.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read human input: %w", err)
	}

	response = strings.TrimSpace(response)

	return &Output{Response: response}, nil
}
