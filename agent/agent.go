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

package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
)

// Agent runs a chat loop using an llm.LLM, optionally with tools and memory.
type Agent struct {
	llm         llm.LLM
	name        string
	description string

	maxIterations int
	tools         []*llm.Tool
	memory        memory.Memory
}

// Result represents the final output of an agent after processing user input and executing any tool calls.
type Result struct {
	Content string
}

// New creates a new Agent.
//
// name and description must be non-empty. client must be non-nil.
func New(client llm.LLM, name, description string) (*Agent, error) {
	if client == nil {
		return nil, ErrUndefinedLLM
	}

	if name == "" {
		return nil, ErrNameRequired
	}

	if description == "" {
		return nil, ErrDescriptionRequired
	}

	return &Agent{
		llm:         client,
		name:        name,
		description: description,
		tools:       make([]*llm.Tool, 0),
	}, nil
}

// Name returns the agent name.
func (a *Agent) Name() string {
	return a.name
}

// Description returns the agent system prompt.
func (a *Agent) Description() string {
	return a.description
}

// AddTool registers a function tool.
//
// It returns ToolAlreadyExistsError if a tool with the same name is already present.
func (a *Agent) AddTool(tool *llm.Tool) error {
	for _, t := range a.tools {
		if t.Name() == tool.Name() {
			return &ToolAlreadyExistsError{Name: tool.Name()}
		}
	}
	a.tools = append(a.tools, tool)
	return nil
}

func (a *Agent) getTool(toolName string) (*llm.Tool, bool) {
	for _, t := range a.tools {
		if t.Name() == toolName {
			return t, true
		}
	}
	return nil, false
}

// SetMemory sets the memory used to seed the agent with previous messages.
func (a *Agent) SetMemory(mem memory.Memory) {
	a.memory = mem
}

// SetMaxIterations sets a maximum number of iterations for the agent loop.
//
// If the limit is reached, Run() returns an error. By default, there is no limit.
func (a *Agent) SetMaxIterations(maxIterations int) {
	a.maxIterations = maxIterations
}

// Run executes the agent loop for the given user input.
//
// The agent calls the LLM, executes any requested tool calls, and repeats until
// the model returns a message without tool calls.
func (a *Agent) Run(ctx context.Context, input string) (*Result, error) {
	session, sessionIndex, err := a.prepareSession(ctx, input)
	if err != nil {
		return nil, err
	}

	// Ensure the session is saved at the end, even if an error occurs during processing.
	defer func() {
		_ = a.saveSession(ctx, session, sessionIndex)
	}()

	iteration := 0
	for {
		iteration++
		if a.maxIterations > 0 && iteration > a.maxIterations {
			return nil, ErrMaxIterationsReached
		}

		var finalMessage *llm.Message
		session, finalMessage, err = a.handleAgentIteration(ctx, session)
		if err != nil {
			return nil, err
		}

		// If finalMessage is nil, it means the agent executed tool calls and needs to call the LLM again.
		if finalMessage != nil {
			return &Result{Content: finalMessage.Content}, nil
		}
	}
}

// saveSession saves the conversation messages to memory, if memory is configured.
func (a *Agent) saveSession(ctx context.Context, messages []llm.Message, sessionIndex int) error {
	if a.memory == nil {
		return nil
	}

	return a.memory.Save(ctx, messages[sessionIndex:])
}

// handleAgentIteration executes one iteration of the agent loop: it calls the LLM with the current messages,
// adds the response to the messages and memory, and executes any tool calls in the response.
func (a *Agent) handleAgentIteration(ctx context.Context, session []llm.Message) ([]llm.Message, *llm.Message, error) {
	msg, err := a.llm.Execute(ctx, session, a.tools)
	if err != nil {
		return session, nil, err
	}

	session = append(session, *msg)

	if len(msg.ToolCalls) == 0 {
		return session, msg, nil
	}

	for _, toolCall := range msg.ToolCalls {
		resultMessage := a.handleToolCall(ctx, toolCall)

		session = append(session, *resultMessage)
	}

	return session, nil, nil
}

// handleToolCall executes a tool call and returns the result as a message to be added to the conversation.
func (a *Agent) handleToolCall(ctx context.Context, toolCall llm.ToolCall) *llm.Message {
	result, err := a.executeToolCall(ctx, toolCall)
	if err != nil {
		result = fmt.Sprintf("Error executing tool '%s': %v", toolCall.Function.Name, err)
	}

	resultMessage := &llm.Message{
		Role:       llm.ChatMessageRoleTool,
		Content:    result,
		ToolCallID: toolCall.ID,
	}

	return resultMessage
}

// prepareSession prepares the messages for the LLM call, including the system prompt, memory messages, and user input.
func (a *Agent) prepareSession(ctx context.Context, input string) ([]llm.Message, int, error) {
	messages := []llm.Message{
		{
			Role:    llm.ChatMessageRoleSystem,
			Content: a.description,
		},
	}

	if a.memory != nil {
		memoryMessages, err := a.memory.Retrieve(ctx, input)
		if err != nil {
			return nil, 0, err
		}

		messages = append(messages, memoryMessages...)
	}

	sessionIndex := len(messages)

	userMessage := llm.Message{
		Role:    llm.ChatMessageRoleUser,
		Content: input,
	}
	messages = append(messages, userMessage)

	return messages, sessionIndex, nil
}

// executeToolCall executes a tool call and returns the result as a string.
func (a *Agent) executeToolCall(ctx context.Context, tc llm.ToolCall) (string, error) {
	tool, found := a.getTool(tc.Function.Name)
	if !found {
		return "", &ToolUnknownError{Name: tc.Function.Name}
	}

	result, err := tool.Handle(ctx, tc.Function.Arguments)
	if err != nil {
		return "", &ToolExecutionError{Name: tc.Function.Name, Err: err}
	}

	resultAsString, isString := result.(string)
	if !isString {
		resultAsBytes, convertErr := json.Marshal(result)
		if convertErr != nil {
			resultAsString = fmt.Sprintf("failed to marshal tool result: %v", convertErr)
		} else {
			resultAsString = string(resultAsBytes)
		}
	}

	return resultAsString, nil
}

// AsTool exports this agent as an OpenAI function tool.
//
// The returned handler keeps an internal message history so repeated tool calls
// act like a continuing conversation with this agent.
//
// The agent's Description is injected as the system prompt by Run().
//
// Tool arguments schema: {"input": "..."}.
func (a *Agent) AsTool(toolName, toolDescription string) (*llm.Tool, error) {
	type ToolInput struct {
		Input string `json:"input" jsonschema:"description=Instructions for the agent. Describe the task, question, or problem the agent should solve."`
	}

	type ToolOutput struct {
		Output string `json:"output" jsonschema:"description=The agent's response"`
	}

	handler := func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		response, err := a.Run(ctx, input.Input)
		if err != nil {
			return nil, err
		}
		return &ToolOutput{Output: response.Content}, nil
	}

	return llm.NewTool(
		toolName,
		toolDescription,
		handler,
	)
}
