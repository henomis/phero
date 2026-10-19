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
	"slices"
	"strings"
	"sync"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	"github.com/henomis/phero/v2/trace"
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
	tracing        *trace.Provider
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
	// Span identifies the handoff span. Run Agent with
	// trace.WithLinks(ctx, h.Span) to link its run back to the handoff when it
	// does not run under the same span.
	Span trace.SpanContext
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

// SetTracing sets the trace provider that receives the agent's spans.
//
// When not set, the provider carried by the run context (trace.WithProvider)
// is used, so sub-agents inherit their parent's provider. Without any
// provider, spans are still recorded for Result.Summary but not exported.
func (a *Agent) SetTracing(p *trace.Provider) {
	a.tracing = p
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
// A run that fails still returns a non-nil result, holding any partial text
// the model produced and the run's Summary (whose StopReason says why it
// ended), together with the error; use errors.Is to tell the cases apart:
//   - ErrMaxIterationsReached when the iteration limit is reached;
//   - ErrStopRun when a tool stopped the run (see StopRun);
//   - ctx.Err() and, when set, the cause given to context.WithCancelCause when
//     ctx is cancelled. ctx is checked before every iteration, so cancelling it
//     (for example from inside a tool) stops the run even when the LLM
//     ignores ctx;
//   - the LLM's or memory's error otherwise (llm.ErrGuardrailBlocked when a
//     guardrail blocked the call).
//
// The session is saved to memory even when ctx is cancelled. If saving fails,
// the result is still returned together with the save error joined via
// errors.Join.
func (a *Agent) Run(ctx context.Context, parts ...llm.ContentPart) (*Result, error) {
	return a.run(ctx, nil, parts...)
}

// runState carries per-run tracing state through the loop.
type runState struct {
	span *trace.Span
	run  *trace.Run
	// promptLen is the prompt length of the previous LLM call, where the next
	// call's message delta starts.
	promptLen int
}

// run executes the agent loop, optionally emitting streaming Events.
//
// When emit is nil the agent runs in buffered mode (identical to the original
// Run). When emit is non-nil, each LLM call is streamed and text/reasoning deltas
// and tool call/result events are pushed through emit as they happen.
func (a *Agent) run(ctx context.Context, emit emitFunc, parts ...llm.ContentPart) (result *Result, err error) {
	if a.tracing != nil {
		ctx = trace.WithProvider(ctx, a.tracing)
	}

	rs := &runState{run: &trace.Run{
		Agent:         a.name,
		Description:   a.description,
		SessionID:     trace.SessionFrom(ctx),
		Input:         parts,
		MaxIterations: max(a.maxIterations, 0),
	}}
	ctx, rs.span = trace.Start(ctx, string(trace.KindRun)+" "+a.name, rs.run)

	var (
		session      []llm.Message
		sessionIndex int
		iteration    int
	)

	defer func() {
		err = a.finishRun(ctx, rs, session, sessionIndex, iteration, result, err)
	}()

	session, sessionIndex, err = a.prepareSession(ctx, parts)
	if err != nil {
		return &Result{}, err
	}

	for {
		if ctx.Err() != nil {
			return partialResultFromSession(session[sessionIndex:]), cancellationError(ctx)
		}

		if a.maxIterations > 0 && iteration >= a.maxIterations {
			return partialResultFromSession(session[sessionIndex:]), ErrMaxIterationsReached
		}

		iteration++

		iterationResult, iterErr := a.handleAgentIteration(ctx, session, iteration, rs, emit)
		if iterErr != nil {
			// An LLM that honours ctx fails on cancellation: report it as a
			// cancelled run, with its partial result and Summary.
			if ctx.Err() != nil {
				return partialResultFromSession(session[sessionIndex:]), cancellationError(ctx)
			}

			return partialResultFromSession(session[sessionIndex:]), iterErr
		}

		session = iterationResult.session

		if iterationResult.stop != nil {
			return iterationResult.result, iterationResult.stop
		}

		// If result is nil, the agent executed tool calls and needs to call the LLM again.
		if iterationResult.result != nil {
			return iterationResult.result, nil
		}
	}
}

// finishRun ends a run: it saves the session to memory, ends the run span and
// attaches its Summary to result. It returns err joined with any save error.
func (a *Agent) finishRun(
	ctx context.Context, rs *runState, session []llm.Message, sessionIndex, iteration int,
	result *Result, err error,
) error {
	rs.run.StopReason = stopReason(ctx, result, err)

	// A stop a tool asked for is how the run was meant to end, not a failure:
	// its span ends without an error, unless saving the session fails.
	var spanErr error
	if rs.run.StopReason != trace.StopRequested {
		spanErr = err
	}

	// A cancelled run still records its turn: saving must not inherit the cancellation.
	if session != nil {
		if saveErr := a.saveSession(context.WithoutCancel(ctx), session, sessionIndex); saveErr != nil {
			saveErr = fmt.Errorf("%w: %w", ErrSessionSaveFailed, saveErr)
			err = errors.Join(err, saveErr)
			spanErr = errors.Join(spanErr, saveErr)
		}
	}

	rs.run.Iterations = iteration
	if result != nil {
		rs.run.Output = result.Parts
		for _, h := range result.Handoffs {
			rs.run.Handoffs = append(rs.run.Handoffs, h.Agent.Name())
		}
	}

	rs.span.End(spanErr)

	if result != nil {
		result.Summary = trace.NewRunSummary(rs.span)
	}

	return err
}

// stopReason classifies how a run ended, before any session save error.
func stopReason(ctx context.Context, result *Result, err error) trace.StopReason {
	switch {
	case errors.Is(err, ErrMaxIterationsReached):
		return trace.StopMaxIterations
	case errors.Is(err, ErrStopRun):
		return trace.StopRequested
	case err != nil && (ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)):
		return trace.StopCancelled
	case errors.Is(err, llm.ErrGuardrailBlocked):
		return trace.StopGuardrail
	case err != nil:
		return trace.StopError
	case result != nil && len(result.Handoffs) > 0:
		return trace.StopHandoff
	default:
		return trace.StopFinal
	}
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
func (a *Agent) saveSession(ctx context.Context, messages []llm.Message, sessionIndex int) error {
	if a.memory == nil {
		return nil
	}

	// An empty answer is not saved: replayed, it would break later runs.
	turn := replayable(messages[sessionIndex:])
	if len(turn) == 0 {
		return nil
	}

	payload := &trace.Memory{Op: trace.MemorySave, Messages: turn}
	ctx, span := trace.Start(ctx, string(trace.KindMemory)+" save", payload)

	err := a.memory.Save(ctx, turn)
	if err == nil {
		payload.Count = len(turn)
	}

	span.End(err)

	return err
}

// agentIteration represents the result of one iteration of the agent loop.
type agentIteration struct {
	session []llm.Message
	// result is the run's final result; nil means the loop must call the LLM again.
	result *Result
	// stop is the StopRun error a tool returned: the run ends with result.
	stop error
}

// handleAgentIteration executes one iteration of the agent loop: it calls the LLM with the current messages,
// adds the response to the messages and memory, and executes any tool calls in the response.
//
// When emit is non-nil the LLM call is streamed and tool call/result events are emitted.
func (a *Agent) handleAgentIteration(
	ctx context.Context, session []llm.Message, iteration int, rs *runState, emit emitFunc,
) (agentIteration, error) {
	var (
		msg *llm.Result
		err error
	)

	if emit == nil {
		callCtx, span, call := a.startLLMCall(ctx, rs, session, iteration, false)
		msg, err = a.llm.Execute(callCtx, session, a.callOptions()...)

		if err == nil && (msg == nil || msg.Message == nil) {
			err = ErrIncompleteStream
		}

		endLLMCall(span, call, msg, err)

		if err != nil {
			return agentIteration{session: session}, err
		}
	} else {
		msg, err = a.streamIteration(ctx, session, iteration, rs, emit)
		if err != nil {
			return agentIteration{session: session}, err
		}
	}

	session = append(session, *msg.Message)

	return a.processToolCalls(ctx, session, msg.Message, iteration, emit)
}

// processToolCalls executes the tool calls in message (if any) concurrently,
// preserving order, and appends their results to the session. When emit is
// non-nil it pushes a ToolCall event before execution and a ToolResult event
// after, both in call order, on the single calling goroutine.
func (a *Agent) processToolCalls(
	ctx context.Context, session []llm.Message, message *llm.Message,
	iteration int, emit emitFunc,
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

	stops := make([]error, len(toolCalls))

	for i, toolCall := range toolCalls {
		go func() {
			defer wg.Done()

			results[i], stops[i] = a.handleToolCall(ctx, toolCall, iteration)
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
			handoffs = append(handoffs, a.handoff(ctx, hAgent, handoffContext(toolCalls[i])))
		}
	}

	// A stop ends the run after this round: every call of the round ran and
	// its result is in the session, as for a handoff.
	if stop := errors.Join(stops...); stop != nil {
		return agentIteration{
			session: session,
			result:  &Result{Parts: nonBlankParts(textOnly(message.Parts))},
			stop:    stop,
		}, nil
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

// handoff records a handoff span and returns the Handoff that refers to it.
func (a *Agent) handoff(ctx context.Context, target *Agent, handoffCtx string) Handoff {
	_, span := trace.Start(ctx, string(trace.KindHandoff)+" "+target.Name(), &trace.Handoff{
		From: a.name, To: target.Name(), Context: handoffCtx,
	})
	span.End(nil)

	return Handoff{Agent: target, Context: handoffCtx, Span: span.Context()}
}

// handleToolCall executes a tool call and returns the result as a message to be added to the conversation,
// and the tool's error when it asked to stop the run (StopRun).
func (a *Agent) handleToolCall(ctx context.Context, toolCall llm.ToolCall, iteration int) (*llm.Message, error) {
	payload := &trace.ToolCall{
		Name:      toolCall.Function.Name,
		CallID:    toolCall.ID,
		Iteration: iteration,
		Arguments: toolCall.Function.Arguments,
	}
	ctx, span := trace.Start(ctx, string(trace.KindTool)+" "+toolCall.Function.Name, payload)

	resultParts, err := a.executeToolCall(ctx, toolCall)

	// A tool that stops the run did what it was asked: its call is no error.
	var stop error
	if errors.Is(err, ErrStopRun) {
		stop, err = err, nil
		resultParts = []llm.ContentPart{llm.Text("The run was stopped: " + stop.Error())}
	}

	if err != nil {
		resultParts = []llm.ContentPart{llm.Text(fmt.Sprintf("Error executing tool '%s': %v", toolCall.Function.Name, err))}
	}

	payload.Result = llm.TextContent(resultParts...)

	span.End(err)

	return &llm.Message{
		Role:       llm.RoleTool,
		Parts:      resultParts,
		ToolCallID: toolCall.ID,
		ToolError:  err != nil,
	}, stop
}

// prepareSession prepares the messages for the LLM call, including the system prompt, memory messages, and user input.
func (a *Agent) prepareSession(ctx context.Context, parts []llm.ContentPart) ([]llm.Message, int, error) {
	messages := []llm.Message{llm.SystemMessage(a.description)}

	if a.memory != nil {
		memoryMessages, err := a.retrieveMemory(ctx, parts)
		if err != nil {
			return nil, 0, err
		}

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

// retrieveMemory loads the replayable messages from memory inside a span.
func (a *Agent) retrieveMemory(ctx context.Context, parts []llm.ContentPart) ([]llm.Message, error) {
	// Use the text representation of the input parts for the memory query.
	payload := &trace.Memory{Op: trace.MemoryRetrieve, Query: llm.TextContent(parts...)}
	ctx, span := trace.Start(ctx, string(trace.KindMemory)+" retrieve", payload)

	memoryMessages, err := a.memory.Retrieve(ctx, payload.Query)
	if err != nil {
		span.End(err)
		return nil, err
	}

	// A memory may hold empty answers saved before they were filtered out.
	memoryMessages = replayable(memoryMessages)
	payload.Messages = memoryMessages
	payload.Count = len(memoryMessages)

	span.End(nil)

	return memoryMessages, nil
}

// replayable returns messages without the assistant messages that carry
// nothing: no tool call, and no text, image or reasoning text. A model
// sometimes answers with nothing at all; kept in memory, that turn is replayed
// on every later run, and OpenAI-compatible providers receive an assistant
// message without content, which some reject (Ollama: 400 "invalid message
// content type: <nil>"), so the agent could never run again. messages is
// returned as is when it holds no such message.
func replayable(messages []llm.Message) []llm.Message {
	if !slices.ContainsFunc(messages, isEmptyAnswer) {
		return messages
	}

	out := make([]llm.Message, 0, len(messages))

	for _, m := range messages {
		if !isEmptyAnswer(m) {
			out = append(out, m)
		}
	}

	return out
}

// isEmptyAnswer reports whether m is an assistant message with nothing to
// replay. A reasoning part counts only for its text: a part that holds a
// provider signature alone gives the model nothing to read.
func isEmptyAnswer(m llm.Message) bool {
	if m.Role != llm.RoleAssistant || len(m.ToolCalls) > 0 {
		return false
	}

	for _, p := range m.Parts {
		switch p.Type {
		case llm.ContentTypeText, llm.ContentTypeReasoning:
			if strings.TrimSpace(p.Text) != "" {
				return false
			}
		case llm.ContentTypeRedactedReasoning:
			if p.Text != "" {
				return false
			}
		case llm.ContentTypeImageURL:
			if p.ImageURL != "" {
				return false
			}
		case llm.ContentTypeImageBase64:
			if p.ImageBase64 != "" {
				return false
			}
		}
	}

	return true
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
// Each call is a new run; set a memory on the agent to make repeated calls
// continue one conversation. The run is traced as a child of the parent's
// tool span, with the parent's provider unless the agent has its own, and its
// usage rolls up into the parent's Result.Summary.
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
