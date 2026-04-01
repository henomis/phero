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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
	"github.com/henomis/phero/trace"
)

// Agent runs a chat loop using an llm.LLM, optionally with tools and memory.
type Agent struct {
	llm         llm.LLM
	name        string
	description string

	maxIterations int
	tools         []*llm.Tool
	memory        memory.Memory
	tracer        trace.Tracer
	handoffs      map[string]*Agent
}

// Result represents the final output of an agent after processing user input and executing any tool calls.
type Result struct {
	Content      string
	HandoffAgent *Agent
	Summary      *trace.RunSummary
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
		tracer:      trace.Noop,
		handoffs:    make(map[string]*Agent),
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
	if _, exists := a.getTool(tool.Name()); exists {
		return &ToolAlreadyExistsError{Name: tool.Name()}
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

// AgentHandoffInput is the structured argument passed to a handoff tool.
type AgentHandoffInput struct {
	Context string `json:"context" jsonschema:"The contextual data gathered by the source agent to be passed to the receiving agent."`
}

// AddTool registers a function tool.
//
// It returns ToolAlreadyExistsError if a tool with the same name is already present.
func (a *Agent) AddHandoff(handoffAgent *Agent) error {
	toolName := fmt.Sprintf("handoff_to_%s", normalizeAgentName(handoffAgent.Name()))

	if _, exists := a.getTool(toolName); exists {
		return &ToolAlreadyExistsError{Name: toolName}
	}

	tool, err := llm.NewTool(
		toolName,
		handoffAgent.Description(),
		func(ctx context.Context, i *AgentHandoffInput) (string, error) {
			return fmt.Sprintf("%s: success", toolName), nil
		},
	)
	if err != nil {
		return err
	}

	a.tools = append(a.tools, tool)
	a.handoffs[toolName] = handoffAgent

	return nil
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

// SetTracer configures the Tracer used to observe agent lifecycle events.
//
// If not set, all events are discarded (trace.Noop is the default).
func (a *Agent) SetTracer(t trace.Tracer) {
	a.tracer = t
}

// Run executes the agent loop for the given user input.
//
// The agent calls the LLM, executes any requested tool calls, and repeats until
// the model returns a message without tool calls.
//
// If the run succeeds but saving the session to memory fails, the result is
// still returned together with the save error joined via errors.Join.
func (a *Agent) Run(ctx context.Context, input string) (result *Result, err error) {
	ctx = trace.WithTracer(ctx, a.tracer)
	ctx = trace.WithAgentName(ctx, a.name)
	stats := newRunStats(a.name)
	handoffAgentName := ""

	session, sessionIndex, err := a.prepareSession(ctx, input, stats)
	if err != nil {
		return nil, err
	}

	a.tracer.Trace(trace.AgentStartEvent{
		AgentName: a.name,
		Input:     input,
		Timestamp: time.Now(),
	})

	iteration := 0

	defer func() {
		if saveErr := a.saveSession(ctx, session, sessionIndex, stats); saveErr != nil {
			err = errors.Join(err, fmt.Errorf("%w: %w", ErrSessionSaveFailed, saveErr))
		}

		output := ""
		if result != nil {
			output = result.Content
		}
		a.tracer.Trace(trace.AgentEndEvent{
			AgentName:  a.name,
			Output:     output,
			Err:        err,
			Iterations: iteration,
			Timestamp:  time.Now(),
		})

		summary := stats.summary(iteration, handoffAgentName, err)
		if result != nil {
			result.Summary = summary
		}

		a.tracer.Trace(trace.AgentRunSummaryEvent{
			Summary:   *summary,
			Timestamp: time.Now(),
		})
	}()

	for {
		iteration++
		if a.maxIterations > 0 && iteration > a.maxIterations {
			return nil, ErrMaxIterationsReached
		}

		a.tracer.Trace(trace.AgentIterationEvent{
			AgentName: a.name,
			Iteration: iteration,
			Timestamp: time.Now(),
		})

		iterCtx := trace.WithIteration(ctx, iteration)

		iterationResult, err := a.handleAgentIteration(iterCtx, session, iteration, stats)
		if err != nil {
			return nil, err
		}

		session = iterationResult.session
		if iterationResult.handoffAgent != nil {
			handoffAgentName = iterationResult.handoffAgent.Name()
		}

		// If finalMessage is nil, it means the agent executed tool calls and needs to call the LLM again.
		if iterationResult.lastMessage != nil {
			return &Result{Content: iterationResult.lastMessage.Content, HandoffAgent: iterationResult.handoffAgent}, nil
		}
	}
}

// saveSession saves the conversation messages to memory, if memory is configured.
func (a *Agent) saveSession(ctx context.Context, messages []llm.Message, sessionIndex int, stats *runStats) error {
	if a.memory == nil {
		return nil
	}

	start := time.Now()
	count := len(messages) - sessionIndex
	err := a.memory.Save(ctx, messages[sessionIndex:])
	duration := time.Since(start)
	if err == nil {
		stats.recordMemorySave(count, duration)
		a.tracer.Trace(trace.MemorySaveEvent{
			AgentName: a.name,
			Count:     count,
			Timestamp: time.Now(),
		})
	} else {
		stats.recordMemorySave(0, duration)
	}
	return err
}

// agentIteration represents the result of one iteration of the agent loop.
type agentIteration struct {
	session      []llm.Message
	lastMessage  *llm.Message
	handoffAgent *Agent
}

// handleAgentIteration executes one iteration of the agent loop: it calls the LLM with the current messages,
// adds the response to the messages and memory, and executes any tool calls in the response.
func (a *Agent) handleAgentIteration(ctx context.Context, session []llm.Message, iteration int, stats *runStats) (agentIteration, error) {
	tracedLLM := trace.NewLLM(a.llm, a.tracer)
	start := time.Now()
	msg, err := tracedLLM.Execute(ctx, session, a.tools)
	duration := time.Since(start)
	if err != nil {
		stats.recordLLM(duration, nil)
		return agentIteration{session: session}, err
	}
	stats.recordLLM(duration, msg.Usage)

	session = append(session, *msg.Message)

	if len(msg.Message.ToolCalls) == 0 {
		return agentIteration{session: session, lastMessage: msg.Message}, nil
	}

	for _, toolCall := range msg.Message.ToolCalls {
		resultMessage := a.handleToolCall(ctx, toolCall, iteration, stats)
		session = append(session, *resultMessage)

		handoffAgent, isHandoff := a.handoffs[toolCall.Function.Name]
		if isHandoff {
			// remove the tool call message from the session, so the handoff agent doesn't see it as input
			return agentIteration{session: session, lastMessage: resultMessage, handoffAgent: handoffAgent}, nil
		}
	}

	return agentIteration{session: session}, nil
}

// handleToolCall executes a tool call and returns the result as a message to be added to the conversation.
func (a *Agent) handleToolCall(ctx context.Context, toolCall llm.ToolCall, iteration int, stats *runStats) *llm.Message {
	a.tracer.Trace(trace.ToolCallEvent{
		AgentName: a.name,
		ToolName:  toolCall.Function.Name,
		Arguments: toolCall.Function.Arguments,
		CallID:    toolCall.ID,
		Iteration: iteration,
		Timestamp: time.Now(),
	})

	start := time.Now()
	result, err := a.executeToolCall(ctx, toolCall)
	stats.recordTool(toolCall.Function.Name, err, time.Since(start))

	a.tracer.Trace(trace.ToolResultEvent{
		AgentName: a.name,
		ToolName:  toolCall.Function.Name,
		Result:    result,
		Err:       err,
		CallID:    toolCall.ID,
		Iteration: iteration,
		Timestamp: time.Now(),
	})

	if err != nil {
		result = fmt.Sprintf("Error executing tool '%s': %v", toolCall.Function.Name, err)
	}

	return &llm.Message{
		Role:       llm.ChatMessageRoleTool,
		Content:    result,
		ToolCallID: toolCall.ID,
	}
}

// prepareSession prepares the messages for the LLM call, including the system prompt, memory messages, and user input.
func (a *Agent) prepareSession(ctx context.Context, input string, stats *runStats) ([]llm.Message, int, error) {
	messages := []llm.Message{
		{
			Role:    llm.ChatMessageRoleSystem,
			Content: a.description,
		},
	}

	if a.memory != nil {
		start := time.Now()
		memoryMessages, err := a.memory.Retrieve(ctx, input)
		duration := time.Since(start)
		if err != nil {
			stats.recordMemoryRetrieve(0, duration)
			return nil, 0, err
		}

		stats.recordMemoryRetrieve(len(memoryMessages), duration)

		a.tracer.Trace(trace.MemoryRetrieveEvent{
			AgentName: a.name,
			Count:     len(memoryMessages),
			Timestamp: time.Now(),
		})

		messages = append(messages, memoryMessages...)
	}

	sessionIndex := len(messages)

	if input != "" {
		userMessage := llm.Message{
			Role:    llm.ChatMessageRoleUser,
			Content: input,
		}
		messages = append(messages, userMessage)
	}

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

func normalizeAgentName(s string) string {
	return strings.ReplaceAll(s, " ", "_")
}
