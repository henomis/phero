package human

import (
	"bufio"
	"context"
	"fmt"
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
}

// New creates a new instance of HumanTool.
//
// This tool enables the agent to request human input during execution.
// The tool will prompt the user with the provided question and return their response.
func New() (*Tool, error) {
	name := "ask_human"
	description := "use this tool to ask a human user for input, clarification, or decision. The input is the question or prompt to show the user. The output is the user's response."

	humanTool := &Tool{
		reader: bufio.NewReader(os.Stdin),
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

// WithReader allows setting a custom reader for the HumanTool (useful for testing).
func (h *Tool) WithReader(reader *bufio.Reader) *Tool {
	h.reader = reader
	return h
}

// Tool returns the llm.Tool representation of the HumanTool.
func (h *Tool) Tool() *llm.Tool {
	return h.tool
}

func (h *Tool) ask(ctx context.Context, input *Input) (*Output, error) {
	_ = ctx
	if input == nil {
		return nil, fmt.Errorf("nil input")
	}
	// Print the question to stdout
	fmt.Printf("\n🤔 Human Input Required:\n%s\n\n", input.Question)
	fmt.Print("Your response: ")

	// Read the user's response
	response, err := h.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read human input: %w", err)
	}

	// Trim whitespace and newline
	response = strings.TrimSpace(response)

	return &Output{Response: response}, nil
}
