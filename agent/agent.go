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
	"sync"
	"time"

	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
	"github.com/henomis/phero/trace"
)

const maxToolNameLength = 64

// DefaultMaxIterations is the iteration limit of an agent created by New.
const DefaultMaxIterations = 25

// Agent runs a chat loop using an llm.LLM, optionally with tools and memory.
type Agent struct {
	llm         llm.LLM
	name        string
	description string

	maxIterations  int
	tools          []*llm.Tool
	responseFormat *llm.ResponseFormat
	memory         memory.Memory
	tracer         trace.Tracer
	handoffs       map[string]*Agent
}

// Result represents the final output of an agent after processing user input and executing any tool calls.
type Result struct {
	// Parts holds the multimodal content of the final assistant message.
	//
	// When the run ends in a handoff, Parts holds the text the model wrote in the
	// message that made the handoff call, and is empty when it wrote none. A
	// payload meant for the next agent belongs in Handoff.Context instead.
	Parts []llm.ContentPart
	// Handoffs lists the handoffs the model made, in call order; more than one
	// means fan-out. It is empty when the run did not end in a handoff.
	Handoffs []Handoff
	Summary  *trace.RunSummary
}

// Handoff is one handoff made by the model at the end of a run.
type Handoff struct {
	// Agent is the agent the work was handed to.
	Agent *Agent
	// Context is what the model passed to Agent in HandoffInput.Context.
	Context string
}

// TextContent returns the concatenation of all text parts in the result.
func (r *Result) TextContent() string {
	if r == nil {
		return ""
	}

	return llm.TextContent(r.Parts...)
}

// ReplyText returns TextContent, or, when that is blank and the run ended in a
// handoff, a note naming each target and the context the model gave it.
//
// Use it where the result is returned to a caller that cannot run the handoff
// targets (a remote client, a parent agent), so a handoff without text does not
// read as an empty answer.
func (r *Result) ReplyText() string {
	if r == nil {
		return ""
	}

	text := r.TextContent()
	if strings.TrimSpace(text) != "" || len(r.Handoffs) == 0 {
		return text
	}

	notes := make([]string, 0, len(r.Handoffs))
	for _, h := range r.Handoffs {
		name := ""
		if h.Agent != nil {
			name = h.Agent.Name()
		}

		note := "handed off to " + name
		if c := strings.TrimSpace(h.Context); c != "" {
			note += ": " + c
		}

		notes = append(notes, note)
	}

	return strings.Join(notes, "\n")
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
		llm:           client,
		name:          name,
		description:   description,
		maxIterations: DefaultMaxIterations,
		tools:         make([]*llm.Tool, 0),
		tracer:        trace.Noop,
		handoffs:      make(map[string]*Agent),
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

// HandoffInput is the structured argument passed to a handoff tool.
type HandoffInput struct {
	Context string `json:"context" jsonschema:"The contextual data gathered by the source agent to be passed to the receiving agent."` //nolint:lll
}

// AddHandoff registers another agent as a handoff target, exposing it as a tool named handoff_to_<name>.
//
// It returns ToolAlreadyExistsError if a tool with the same name is already present.
func (a *Agent) AddHandoff(handoffAgent *Agent) error {
	toolName := fmt.Sprintf("handoff_to_%s", SanitizeToolName(handoffAgent.Name()))

	if _, exists := a.getTool(toolName); exists {
		return &ToolAlreadyExistsError{Name: toolName}
	}

	tool, err := llm.NewTool(
		toolName,
		handoffAgent.Description(),
		func(_ context.Context, _ *HandoffInput) (string, error) {
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
// If the limit is reached, Run() returns ErrMaxIterationsReached together with
// a non-nil result holding any partial text the model produced so far and the
// run's Summary. The default is DefaultMaxIterations; zero or a negative value
// removes the limit.
func (a *Agent) SetMaxIterations(maxIterations int) {
	a.maxIterations = maxIterations
}

// SetTracer configures the Tracer used to observe agent lifecycle events.
//
// If not set, all events are discarded (trace.Noop is the default).
func (a *Agent) SetTracer(t trace.Tracer) {
	a.tracer = t
}

// SetResponseFormat constrains the agent's final answer to a JSON schema
// (structured output); its text is then a JSON document matching the format.
// Tool calls are unaffected: the model still calls tools freely, and the format
// applies to the answer it gives once it stops. Pass nil to restore free text.
func (a *Agent) SetResponseFormat(format *llm.ResponseFormat) {
	a.responseFormat = format
}

// callOptions returns the per-call LLM options every iteration of the loop sends.
func (a *Agent) callOptions() []llm.CallOption {
	opts := []llm.CallOption{llm.WithTools(a.tools...)}
	if a.responseFormat != nil {
		opts = append(opts, llm.WithResponseFormat(a.responseFormat))
	}

	return opts
}

// Run executes the agent loop for the given user input parts.
//
// Call with llm.Text("hello") for a plain-text message, or mix llm.Text and
// llm.ImageURL parts for multimodal input.
//
// The agent calls the LLM, executes any requested tool calls, and repeats until
// the model returns a message without tool calls.
//
// If the maximum iterations limit is reached, the function returns
// ErrMaxIterationsReached together with a non-nil result holding any partial
// text the model produced and the run's Summary (use errors.Is to distinguish).
//
// ctx is checked before every iteration, so cancelling it (for example from
// inside a tool) stops the run even when the LLM ignores ctx. A cancelled run
// returns a non-nil partial result, like ErrMaxIterationsReached, and an error
// matching ctx.Err() and, when set, the cause given to context.WithCancelCause.
//
// The session is saved to memory even when ctx is cancelled. If saving fails,
// the result is still returned together with the save error joined via
// errors.Join.
func (a *Agent) Run(ctx context.Context, parts ...llm.ContentPart) (*Result, error) {
	return a.run(ctx, nil, parts...)
}

// run executes the agent loop, optionally emitting streaming Events.
//
// When emit is nil the agent runs in buffered mode (identical to the original
// Run). When emit is non-nil, each LLM call is streamed and text/reasoning deltas
// and tool call/result events are pushed through emit as they happen.
func (a *Agent) run(ctx context.Context, emit emitFunc, parts ...llm.ContentPart) (result *Result, err error) {
	ctx = trace.WithTracer(ctx, a.tracer)
	ctx = trace.WithAgentName(ctx, a.name)
	stats := newRunStats(a.name)

	var handoffAgentNames []string

	session, sessionIndex, err := a.prepareSession(ctx, parts, stats)
	if err != nil {
		return nil, err
	}

	inputText := llm.TextContent(parts...)
	a.tracer.Trace(trace.AgentStartEvent{
		AgentName: a.name,
		Input:     inputText,
		Timestamp: time.Now(),
	})

	iteration := 0

	defer func() {
		err = a.finishRun(ctx, session, sessionIndex, stats, iteration, handoffAgentNames, result, err)
	}()

	for {
		if ctx.Err() != nil {
			return partialResultFromSession(session[sessionIndex:]), cancellationError(ctx)
		}

		iteration++
		if a.maxIterations > 0 && iteration > a.maxIterations {
			return partialResultFromSession(session[sessionIndex:]), ErrMaxIterationsReached
		}

		a.tracer.Trace(trace.AgentIterationEvent{
			AgentName: a.name,
			Iteration: iteration,
			Timestamp: time.Now(),
		})

		iterCtx := trace.WithIteration(ctx, iteration)

		iterationResult, iterErr := a.handleAgentIteration(iterCtx, session, iteration, stats, emit)
		if iterErr != nil {
			// An LLM that honours ctx fails on cancellation: report it as a
			// cancelled run, with its partial result and Summary.
			if ctx.Err() != nil {
				return partialResultFromSession(session[sessionIndex:]), cancellationError(ctx)
			}

			return nil, iterErr
		}

		session = iterationResult.session

		// If result is nil, the agent executed tool calls and needs to call the LLM again.
		if iterationResult.result != nil {
			for _, h := range iterationResult.result.Handoffs {
				handoffAgentNames = append(handoffAgentNames, h.Agent.Name())
			}

			return iterationResult.result, nil
		}
	}
}

// finishRun ends a run: it saves the session to memory, traces the end of the
// run and attaches its Summary to result. It returns err joined with any save
// error.
func (a *Agent) finishRun(
	ctx context.Context, session []llm.Message, sessionIndex int, stats *runStats,
	iteration int, handoffAgentNames []string, result *Result, err error,
) error {
	// A cancelled run still records its turn: saving must not inherit the cancellation.
	if saveErr := a.saveSession(context.WithoutCancel(ctx), session, sessionIndex, stats); saveErr != nil {
		err = errors.Join(err, fmt.Errorf("%w: %w", ErrSessionSaveFailed, saveErr))
	}

	output := ""
	if result != nil {
		output = result.TextContent()
	}

	a.tracer.Trace(trace.AgentEndEvent{
		AgentName:  a.name,
		Output:     output,
		Err:        err,
		Iterations: iteration,
		Timestamp:  time.Now(),
	})

	summary := stats.summary(iteration, handoffAgentNames, err)
	if result != nil {
		result.Summary = summary
	}

	a.tracer.Trace(trace.AgentRunSummaryEvent{
		Summary:   *summary,
		Timestamp: time.Now(),
	})

	return err
}

// partialResultFromSession scans the messages of the current run backwards and
// returns a Result built from the last assistant message that contains at least
// one text part. The Result is never nil, so the caller still gets the run's
// Summary; its Parts are empty when the model wrote no text.
func partialResultFromSession(session []llm.Message) *Result {
	for i := len(session) - 1; i >= 0; i-- {
		msg := session[i]
		if msg.Role != llm.RoleAssistant {
			continue
		}

		if textParts := textOnly(msg.Parts); len(textParts) > 0 {
			return &Result{Parts: textParts}
		}
	}

	return &Result{}
}

// cancellationError returns ctx.Err(), wrapped together with the cancellation
// cause when one was set with context.WithCancelCause, so errors.Is matches both.
func cancellationError(ctx context.Context) error {
	err := ctx.Err()
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, err) {
		return fmt.Errorf("%w: %w", err, cause)
	}

	return err
}

// textOnly returns the text parts of parts, dropping reasoning and media.
func textOnly(parts []llm.ContentPart) []llm.ContentPart {
	out := make([]llm.ContentPart, 0, len(parts))

	for _, p := range parts {
		if p.Type == llm.ContentTypeText {
			out = append(out, p)
		}
	}

	return out
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
	session []llm.Message
	// result is the run's final result; nil means the loop must call the LLM again.
	result *Result
}

// handleAgentIteration executes one iteration of the agent loop: it calls the LLM with the current messages,
// adds the response to the messages and memory, and executes any tool calls in the response.
//
// When emit is non-nil the LLM call is streamed and tool call/result events are emitted.
func (a *Agent) handleAgentIteration(
	ctx context.Context, session []llm.Message, iteration int, stats *runStats, emit emitFunc,
) (agentIteration, error) {
	var (
		msg *llm.Result
		err error
	)

	if emit == nil {
		tracedLLM := trace.NewLLM(a.llm, a.tracer)
		start := time.Now()
		msg, err = tracedLLM.Execute(ctx, session, a.callOptions()...)

		duration := time.Since(start)
		if err != nil {
			stats.recordLLM(duration, "", nil)
			return agentIteration{session: session}, err
		}

		stats.recordLLM(duration, msg.Model, msg.Usage)
	} else {
		msg, err = a.streamIteration(ctx, session, iteration, stats, emit)
		if err != nil {
			return agentIteration{session: session}, err
		}
	}

	session = append(session, *msg.Message)

	return a.processToolCalls(ctx, session, msg.Message, iteration, stats, emit)
}

// processToolCalls executes the tool calls in message (if any) concurrently,
// preserving order, and appends their results to the session. When emit is
// non-nil it pushes a ToolCall event before execution and a ToolResult event
// after, both in call order, on the single calling goroutine.
func (a *Agent) processToolCalls(
	ctx context.Context, session []llm.Message, message *llm.Message,
	iteration int, stats *runStats, emit emitFunc,
) (agentIteration, error) {
	toolCalls := message.ToolCalls
	if len(toolCalls) == 0 {
		return agentIteration{session: session, result: &Result{Parts: message.Parts}}, nil
	}

	if emit != nil {
		for _, toolCall := range toolCalls {
			emit(Event{
				Type:       EventToolCall,
				ToolCallID: toolCall.ID,
				ToolName:   toolCall.Function.Name,
				ToolArgs:   toolCall.Function.Arguments,
				Iteration:  iteration,
			})
		}
	}

	// Execute all tool calls concurrently, preserving order.
	results := make([]*llm.Message, len(toolCalls))

	var wg sync.WaitGroup
	wg.Add(len(toolCalls))

	for i, toolCall := range toolCalls {
		go func() {
			defer wg.Done()

			results[i] = a.handleToolCall(ctx, toolCall, iteration, stats)
		}()
	}

	wg.Wait()

	if emit != nil {
		for i, result := range results {
			emit(Event{
				Type:       EventToolResult,
				ToolCallID: toolCalls[i].ID,
				ToolName:   toolCalls[i].Function.Name,
				ToolResult: llm.TextContent(result.Parts...),
				ToolError:  result.ToolError,
				Iteration:  iteration,
			})
		}
	}

	// Append results in order; collect all handoffs (fan-out when more than one).
	var handoffs []Handoff

	for i, result := range results {
		session = append(session, *result)
		if hAgent, ok := a.handoffs[toolCalls[i].Function.Name]; ok {
			handoffs = append(handoffs, Handoff{Agent: hAgent, Context: handoffContext(toolCalls[i])})
		}
	}

	if len(handoffs) > 0 {
		// All tool results are preserved in session. The result carries the text the
		// model wrote alongside the handoff calls, not the handoff tools' replies.
		return agentIteration{
			session: session,
			result:  &Result{Parts: nonBlankParts(textOnly(message.Parts)), Handoffs: handoffs},
		}, nil
	}

	return agentIteration{session: session}, nil
}

// handoffContext returns the context argument of a handoff tool call.
//
// Malformed arguments yield an empty context: the handoff tool itself already
// failed on them, and that error is recorded in the session as its tool result.
func handoffContext(tc llm.ToolCall) string {
	var input HandoffInput
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
		return ""
	}

	return input.Context
}

// handleToolCall executes a tool call and returns the result as a message to be added to the conversation.
func (a *Agent) handleToolCall(
	ctx context.Context, toolCall llm.ToolCall, iteration int, stats *runStats,
) *llm.Message {
	a.tracer.Trace(trace.ToolCallEvent{
		AgentName: a.name,
		ToolName:  toolCall.Function.Name,
		Arguments: toolCall.Function.Arguments,
		CallID:    toolCall.ID,
		Iteration: iteration,
		Timestamp: time.Now(),
	})

	start := time.Now()
	resultParts, err := a.executeToolCall(ctx, toolCall)
	stats.recordTool(toolCall.Function.Name, err, time.Since(start))

	// Trace with text representation.
	traceResult := llm.TextContent(resultParts...)
	if err != nil {
		traceResult = err.Error()
	}

	a.tracer.Trace(trace.ToolResultEvent{
		AgentName: a.name,
		ToolName:  toolCall.Function.Name,
		Result:    traceResult,
		Err:       err,
		CallID:    toolCall.ID,
		Iteration: iteration,
		Timestamp: time.Now(),
	})

	if err != nil {
		resultParts = []llm.ContentPart{llm.Text(fmt.Sprintf("Error executing tool '%s': %v", toolCall.Function.Name, err))}
	}

	return &llm.Message{
		Role:       llm.RoleTool,
		Parts:      resultParts,
		ToolCallID: toolCall.ID,
		ToolError:  err != nil,
	}
}

// prepareSession prepares the messages for the LLM call, including the system prompt, memory messages, and user input.
func (a *Agent) prepareSession(
	ctx context.Context, parts []llm.ContentPart, stats *runStats,
) ([]llm.Message, int, error) {
	messages := []llm.Message{llm.SystemMessage(a.description)}

	// Use the text representation of the input parts for the memory query.
	inputText := llm.TextContent(parts...)

	if a.memory != nil {
		start := time.Now()
		memoryMessages, err := a.memory.Retrieve(ctx, inputText)

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

	// A run with no input continues the conversation already in memory, as when a
	// handoff target picks up where the source agent left off. Blank text counts
	// as no input: an empty user turn is rejected by providers ("invalid message
	// content type") rather than ignored.
	if content := nonBlankParts(parts); len(content) > 0 {
		messages = append(messages, llm.UserMessage(content...))
	}

	return messages, sessionIndex, nil
}

// nonBlankParts returns parts without its whitespace-only text parts.
func nonBlankParts(parts []llm.ContentPart) []llm.ContentPart {
	out := make([]llm.ContentPart, 0, len(parts))

	for _, p := range parts {
		if p.Type == llm.ContentTypeText && strings.TrimSpace(p.Text) == "" {
			continue
		}

		out = append(out, p)
	}

	return out
}

// executeToolCall executes a tool call and returns the result as content parts.
func (a *Agent) executeToolCall(ctx context.Context, tc llm.ToolCall) ([]llm.ContentPart, error) {
	tool, found := a.getTool(tc.Function.Name)
	if !found {
		return nil, &ToolUnknownError{Name: tc.Function.Name}
	}

	result, err := tool.Handle(ctx, tc.Function.Arguments)
	if err != nil {
		return nil, &ToolExecutionError{Name: tc.Function.Name, Err: err}
	}

	return anyToContentParts(result), nil
}

// anyToContentParts converts any tool result value to a slice of ContentParts.
//
// If the value is already []llm.ContentPart it is returned directly.
// A plain string becomes a single text part.
// All other types are JSON-marshalled into a text part.
func anyToContentParts(v any) []llm.ContentPart {
	if v == nil {
		return nil
	}

	if parts, ok := v.([]llm.ContentPart); ok {
		return parts
	}

	if s, ok := v.(string); ok {
		return []llm.ContentPart{llm.Text(s)}
	}

	b, err := json.Marshal(v)
	if err != nil {
		return []llm.ContentPart{llm.Text(fmt.Sprintf("failed to marshal tool result: %v", err))}
	}

	return []llm.ContentPart{llm.Text(string(b))}
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
		Input string `json:"input" jsonschema:"description=Instructions for the agent. Describe the task, question, or problem the agent should solve."` //nolint:lll
	}

	type ToolOutput struct {
		Output string `json:"output" jsonschema:"description=The agent's response"`
	}

	handler := func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		response, err := a.Run(ctx, llm.Text(input.Input))
		if err != nil {
			return nil, err
		}

		return &ToolOutput{Output: response.ReplyText()}, nil
	}

	return llm.NewTool(
		toolName,
		toolDescription,
		handler,
	)
}

// SanitizeToolName maps any string to one accepted by LLM providers.
// Only [a-zA-Z0-9_-] are kept (all others become '_'); capped at 64 chars;
// empty result falls back to "agent".
func SanitizeToolName(name string) string {
	s := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}

		return '_'
	}, name)
	if len(s) > maxToolNameLength {
		s = s[:maxToolNameLength]
	}

	if s == "" {
		s = "agent"
	}

	return s
}
