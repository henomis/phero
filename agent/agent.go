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
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
	"github.com/henomis/phero/v2/trace"
)

const maxToolNameLength = 64

// DefaultMaxIterations is the iteration limit of an agent created by New.
// It suits short tool-using tasks; a long-running agent, such as a coding
// agent that reads, edits and tests over many turns, needs a higher limit set
// with SetMaxIterations.
const DefaultMaxIterations = 25

// TruncatedToolCallResult is the error result given to each tool call of a
// response cut off at the output limit. The calls are not run, because their
// arguments are probably cut too.
const TruncatedToolCallResult = "The call's arguments were cut off at the output limit and it was not run. " +
	"Retry with smaller arguments, for example write the file in several edits."

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

	compactor       Compactor
	compactAtTokens int
	checkpoint      bool

	maxParallelTools int
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
	// FinishReason is why the last LLM call stopped. llm.FinishLength means
	// the answer in Parts was cut off at the output limit; the app may ask the
	// model to continue. It is empty when the provider reports no reason.
	FinishReason llm.FinishReason
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

// SetLLM replaces the model the agent calls, keeping its tools, handoffs,
// memory and every other setting, so the next run continues the same
// conversation with another model. Call it between runs, not during one. It
// returns ErrUndefinedLLM for a nil client.
func (a *Agent) SetLLM(client llm.LLM) error {
	if client == nil {
		return ErrUndefinedLLM
	}

	a.llm = client

	return nil
}

// SetMemory sets the memory used to seed the agent with previous messages.
func (a *Agent) SetMemory(mem memory.Memory) {
	a.memory = mem
}

// SetCheckpoint makes the agent save the session to memory after every
// iteration that ran tool calls, instead of only when the run ends, so a
// process that crashes in a long run loses at most the iteration in progress.
// Only the messages added since the previous save are saved; an assistant
// message is always saved together with the results of its tool calls. The
// next run continues from what memory holds (Run with no input picks up where
// the last saved iteration left off).
//
// With compaction (SetCompaction), the messages already saved stay in memory
// as they were, and only the messages added after a compaction are saved.
//
// It has no effect on an agent without memory.
func (a *Agent) SetCheckpoint(enabled bool) {
	a.checkpoint = enabled
}

// SetMaxParallelTools caps how many tool calls of one round run at once; 0
// or less (the default) runs them all at once. Tools marked exclusive with
// llm.Tool.SetExclusive always run alone.
func (a *Agent) SetMaxParallelTools(n int) {
	a.maxParallelTools = max(n, 0)
}

// SetMaxIterations sets a maximum number of iterations for the agent loop.
//
// If the limit is reached, Run() returns ErrMaxIterationsReached together with
// a non-nil result holding any partial text the model produced so far and the
// run's Summary. The default is DefaultMaxIterations; zero or a negative value
// removes the limit. An agent that works for many turns, such as a coding
// agent, should raise it (200 or more) or remove it and bound the run with a
// context deadline instead.
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

// SetCompaction makes the agent compact its session during a run, so a long
// run keeps fitting the model's context window. The session, without the
// system prompt and the current summary, is handed to c (NewSummaryCompactor
// is the default choice), and becomes [system, the summary as a user message,
// its pinned messages, the kept messages]. It is compacted:
//   - before an LLM call, when the previous call's prompt and output tokens,
//     as the provider reported them, reached atTokens; 0 disables this check.
//     Before any size is reported (the first call of a run, after a
//     compaction, or with a provider that reports no usage), the session is
//     estimated with EstimateTokens, so a long stored history is compacted
//     before it is sent;
//   - when a call fails with llm.ErrContextLengthExceeded: the session is
//     compacted and the call made again, once. A second such error in a row
//     ends the run.
//
// Each compaction is traced as a memory span with op trace.MemoryCompact.
// Memory keeps every message once: before compacting, the run saves the
// messages it has not saved yet, and after it, only the messages added later.
// With a memory.SnapshotStore, the compaction's snapshot is stored too
// (traced as op trace.MemorySnapshot), and the next run starts from it: the
// summary, then only the messages after it. Storing it fails without failing
// the run when another writer stored a snapshot first, or the log no longer
// holds the compacted messages; the summary then lives in the run only, as it
// does with any other memory, and the next run loads the full history and
// compacts again when it needs to.
//
// Pass a nil Compactor to disable compaction.
func (a *Agent) SetCompaction(c Compactor, atTokens int) {
	a.compactor = c
	a.compactAtTokens = max(atTokens, 0)
}

// compactIfNeeded compacts the session before the next LLM call when the
// previous call reached the compaction threshold.
func (a *Agent) compactIfNeeded(ctx context.Context, rs *runState, session *[]llm.Message, sessionIndex *int) error {
	if a.compactor == nil || a.compactAtTokens <= 0 {
		return nil
	}

	// Before the provider has reported a prompt size (the first call, after a
	// compaction, or with no usage at all), the session is estimated: a run
	// that starts with a long history must not rely on the provider failing
	// with a recognizable context-length error.
	tokens := rs.contextTokens
	if tokens == 0 {
		tokens = EstimateTokens(*session)
	}

	if tokens < a.compactAtTokens {
		return nil
	}

	// Compaction only shortens the messages after the summary. When those fit
	// the budget a compactor keeps whole, the threshold is reached by what it
	// can't shorten (the system prompt, the summary, the pinned task), and
	// compacting again would cost a summary call on every iteration for
	// nothing. A prompt that then overflows is still compacted once on the
	// context-length error.
	if EstimateTokens((*session)[rs.tailStart():]) < a.keepTokens() {
		return nil
	}

	var err error

	*session, *sessionIndex, err = a.compactSession(ctx, rs, *session, *sessionIndex)

	return err
}

// keepTokens returns the budget for the messages a compaction keeps whole: a
// quarter of the compaction threshold.
func (a *Agent) keepTokens() int {
	return a.compactAtTokens / 4 //nolint:mnd // a quarter of the threshold for the kept messages
}

// iterationFailed decides how the run goes on after an iteration failed with
// iterErr. It returns true when the call should be made again, after the
// session was compacted for a prompt too long for the context window, and
// otherwise the error the run ends with: the cancellation when ctx is done (an
// LLM that honours ctx fails on cancellation), else iterErr.
func (a *Agent) iterationFailed(
	ctx context.Context, rs *runState, session *[]llm.Message, sessionIndex *int, iterErr error,
) (bool, error) {
	if ctx.Err() != nil {
		return false, cancellationError(ctx)
	}

	retry, compactErr := a.compactAfterError(ctx, rs, session, sessionIndex, iterErr)
	if retry {
		return true, nil
	}

	if compactErr != nil {
		return false, errors.Join(iterErr, compactErr)
	}

	return false, iterErr
}

// compactAfterError compacts the session after a call failed because its
// prompt is too long for the context window, once in a row, and reports
// whether the call should be made again. It returns the compaction's error
// when compacting failed.
func (a *Agent) compactAfterError(
	ctx context.Context, rs *runState, session *[]llm.Message, sessionIndex *int, callErr error,
) (bool, error) {
	if a.compactor == nil || rs.compactedOnError || !errors.Is(callErr, llm.ErrContextLengthExceeded) {
		return false, nil
	}

	rs.compactedOnError = true

	var err error

	*session, *sessionIndex, err = a.compactSession(ctx, rs, *session, *sessionIndex)

	return err == nil, err
}

// compactSession compacts the session after its system prompt and its
// current summary, inside a memory span. It returns the new session,
// [system, summary messages, kept messages], and the index where the kept
// messages start (where a cancelled run's partial result is read from). On
// error, or when there is nothing to compact, the session is returned
// unchanged.
func (a *Agent) compactSession(
	ctx context.Context, rs *runState, session []llm.Message, sessionIndex int,
) ([]llm.Message, int, error) {
	payload := &trace.Memory{Op: trace.MemoryCompact}
	ctx, span := trace.Start(ctx, string(trace.KindMemory)+" compact", payload)

	in := CompactInput{
		Previous:   rs.snapshot,
		Messages:   slices.Clone(session[rs.tailStart():]),
		KeepTokens: a.keepTokens(),
	}

	compaction, err := a.compactor.Compact(ctx, in)
	if err == nil && compaction != nil {
		err = compaction.check(in)
	}

	if err != nil || compaction == nil {
		span.End(err)

		return session, sessionIndex, err
	}

	// Memory only appends: the messages not saved yet are saved now, as they
	// are, and the compacted ones are never saved. Each message is then stored
	// once, and the summary lives only in this run.
	if err = a.saveSession(context.WithoutCancel(ctx), session, rs.savedTo); err != nil {
		err = fmt.Errorf("%w: %w", ErrSessionSaveFailed, err)
		span.End(err)

		return session, sessionIndex, err
	}

	if store, ok := a.memory.(memory.SnapshotStore); ok {
		a.storeSnapshot(ctx, store, rs, in, compaction)
	}

	summary := memory.SummaryMessages(compaction.Snapshot)

	newSession := make([]llm.Message, 0, 1+len(summary)+len(compaction.Kept))
	newSession = append(newSession, session[0])
	newSession = append(newSession, summary...)
	newSession = append(newSession, compaction.Kept...)

	payload.Messages = newSession[1:]
	payload.Count = len(newSession) - 1

	span.End(nil)

	rs.snapshot = &compaction.Snapshot

	// The next call's prompt size is unknown until the provider reports it,
	// and its trace payload records the whole new session.
	rs.contextTokens = 0
	rs.promptLen = 0
	rs.savedTo = len(newSession)

	return newSession, rs.tailStart(), nil
}

// storeSnapshot stores the compaction's snapshot in store, inside a memory
// span, so the next run starts from it. The session's messages are all in the
// log by now; the snapshot covers them up to the last one not kept. Failing
// to store it does not fail the run: the snapshot then lives in the run only,
// as with a plain memory, and the error is recorded on the span.
func (a *Agent) storeSnapshot(
	ctx context.Context, store memory.SnapshotStore, rs *runState, in CompactInput, compaction *Compaction,
) {
	payload := &trace.Memory{Op: trace.MemorySnapshot}
	ctx, span := trace.Start(context.WithoutCancel(ctx), string(trace.KindMemory)+" snapshot", payload)

	upTo, err := snapshotCursor(ctx, store, rs, in, compaction)
	if err == nil {
		err = store.SaveSnapshot(ctx, compaction.Snapshot, upTo, rs.stored)
	}

	if err != nil {
		span.End(err)
		return
	}

	saved := compaction.Snapshot
	saved.UpTo = upTo
	rs.stored = &saved

	payload.Messages = memory.SummaryMessages(saved)
	payload.Count = len(payload.Messages)

	span.End(nil)
}

// snapshotCursor returns the cursor of the last message the compaction's
// snapshot covers, by finding the compacted messages at the end of the log.
func snapshotCursor(
	ctx context.Context, store memory.SnapshotStore, rs *runState, in CompactInput, compaction *Compaction,
) (memory.Cursor, error) {
	current, logged, err := store.LoadSnapshot(ctx)
	if err != nil {
		return "", err
	}

	if !memory.SameSnapshot(current, rs.stored) {
		return "", memory.ErrSnapshotConflict
	}

	// Empty answers are not saved: match the messages the log can hold.
	covered := replayable(in.Messages[:len(in.Messages)-len(compaction.Kept)])
	tail := replayable(in.Messages)

	loggedMsgs := make([]memory.LoggedMessage, 0, len(logged))
	for _, lm := range logged {
		if !isEmptyAnswer(lm.Message) {
			loggedMsgs = append(loggedMsgs, lm)
		}
	}

	if len(covered) == 0 {
		return "", fmt.Errorf("%w: it covers no saved message", ErrSnapshotMismatch)
	}

	// The tail is at the end of the log, or followed by another writer's
	// messages.
	for start := len(loggedMsgs) - len(tail); start >= 0; start-- {
		if alignedWithLog(loggedMsgs[start:start+len(tail)], tail) {
			return loggedMsgs[start+len(covered)-1].Cursor, nil
		}
	}

	return "", ErrSnapshotMismatch
}

// alignedWithLog reports whether logged holds msgs, one for one. A tool
// result is matched by its call ID, not its text: a compaction may have
// clipped the session's copy (DefaultKeptToolResultChars), while the log keeps
// the original.
func alignedWithLog(logged []memory.LoggedMessage, msgs []llm.Message) bool {
	for i, m := range msgs {
		l := logged[i].Message
		if l.Role != m.Role || l.ToolCallID != m.ToolCallID || !sameToolCalls(l, m) {
			return false
		}

		if m.Role != llm.RoleTool && l.TextContent() != m.TextContent() {
			return false
		}
	}

	return true
}

// tailStart returns the index of the session's first message after the
// system prompt and the summary messages.
func (rs *runState) tailStart() int {
	if rs.snapshot == nil {
		return 1
	}

	return 1 + len(memory.SummaryMessages(*rs.snapshot))
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
	// snapshot is the summary the session starts from, after the system
	// prompt (nil for none): the session is [system, SummaryMessages(snapshot),
	// tail], and only the tail is compacted again.
	snapshot *memory.Snapshot
	// stored is the latest snapshot the memory holds as far as this run
	// knows: what a SaveSnapshot compares against. It lags snapshot when
	// storing one failed.
	stored *memory.Snapshot
	// contextTokens estimates the tokens of the next prompt: the previous
	// call's prompt and output tokens, as the provider reported them. It is 0
	// when unknown, as after a compaction, and EstimateTokens is used then.
	contextTokens int
	// compactedOnError is set when the session was compacted after a
	// context-length error, so a second such error in a row ends the run.
	compactedOnError bool
	// savedTo is the index of the first session message not saved to memory.
	savedTo int
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

	// Tool calls see the run they belong to (llm.RunFromContext). It ends when
	// the run returns, after finishRun, which is deferred later.
	ctx, endRun := llm.ContextWithRun(ctx, rand.Text())
	defer endRun()

	var (
		session      []llm.Message
		sessionIndex int
		iteration    int
	)

	defer func() {
		err = a.finishRun(ctx, rs, session, iteration, result, err)
	}()

	session, sessionIndex, err = a.prepareSession(ctx, rs, parts)
	if err != nil {
		return &Result{}, err
	}

	rs.savedTo = sessionIndex

	for {
		if ctx.Err() != nil {
			return partialResultFromSession(session[sessionIndex:]), cancellationError(ctx)
		}

		if a.maxIterations > 0 && iteration >= a.maxIterations {
			return partialResultFromSession(session[sessionIndex:]), ErrMaxIterationsReached
		}

		if err = a.compactIfNeeded(ctx, rs, &session, &sessionIndex); err != nil {
			return partialResultFromSession(session[sessionIndex:]), err
		}

		iteration++

		iterationResult, iterErr := a.handleAgentIteration(ctx, session, iteration, rs, emit)
		if iterErr != nil {
			if retry, failErr := a.iterationFailed(ctx, rs, &session, &sessionIndex, iterErr); !retry {
				return partialResultFromSession(session[sessionIndex:]), failErr
			}

			continue
		}

		rs.compactedOnError = false
		session = iterationResult.session

		if done, doneResult, doneErr := a.afterIteration(ctx, rs, iterationResult, sessionIndex); done {
			return doneResult, doneErr
		}
	}
}

// afterIteration reports whether the run ends after a successful iteration,
// with its result and error: when a tool stopped the run, when the model
// answered without tool calls, or when saving a checkpoint failed. Otherwise
// the agent ran tool calls and calls the LLM again.
func (a *Agent) afterIteration(
	ctx context.Context, rs *runState, it agentIteration, sessionIndex int,
) (bool, *Result, error) {
	if it.stop != nil {
		return true, it.result, it.stop
	}

	if it.result != nil {
		return true, it.result, nil
	}

	if err := a.saveCheckpoint(ctx, rs, it.session); err != nil {
		return true, partialResultFromSession(it.session[sessionIndex:]), err
	}

	return false, nil, nil
}

// saveCheckpoint saves the messages added since the previous save, when
// checkpoints are enabled (SetCheckpoint).
func (a *Agent) saveCheckpoint(ctx context.Context, rs *runState, session []llm.Message) error {
	if !a.checkpoint || a.memory == nil {
		return nil
	}

	// As at the end of a run, a cancelled run still records its iteration.
	if err := a.saveSession(context.WithoutCancel(ctx), session, rs.savedTo); err != nil {
		return fmt.Errorf("%w: %w", ErrSessionSaveFailed, err)
	}

	rs.savedTo = len(session)

	return nil
}

// finishRun ends a run: it saves the session to memory, ends the run span and
// attaches its Summary to result. It returns err joined with any save error.
func (a *Agent) finishRun(
	ctx context.Context, rs *runState, session []llm.Message, iteration int,
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
		if saveErr := a.saveSession(context.WithoutCancel(ctx), session, rs.savedTo); saveErr != nil {
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

	if u := msg.Usage; u != nil {
		rs.contextTokens = u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens + u.OutputTokens
	}

	session = append(session, *msg.Message)

	return a.processToolCalls(ctx, session, msg.Message, msg.FinishReason, iteration, emit)
}

// processToolCalls executes the tool calls in message (if any) concurrently,
// preserving order, and appends their results to the session. When emit is
// non-nil it pushes the ToolCall events, in call order, before execution, and
// each ToolResult event as soon as its call finishes, all on the single
// calling goroutine.
//
// When the response was cut off at the output limit (finish is
// llm.FinishLength), its tool calls are not run: their arguments are probably
// cut too. Each gets an error result asking the model to retry.
func (a *Agent) processToolCalls(
	ctx context.Context, session []llm.Message, message *llm.Message,
	finish llm.FinishReason, iteration int, emit emitFunc,
) (agentIteration, error) {
	toolCalls := message.ToolCalls
	if len(toolCalls) == 0 {
		return agentIteration{session: session, result: &Result{Parts: message.Parts, FinishReason: finish}}, nil
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

	if finish == llm.FinishLength {
		return agentIteration{session: append(session, truncatedToolResults(toolCalls, iteration, emit)...)}, nil
	}

	results, stops := a.runToolCalls(ctx, toolCalls, iteration, emit)

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
			result:  &Result{Parts: nonBlankParts(textOnly(message.Parts)), FinishReason: finish},
			stop:    stop,
		}, nil
	}

	if len(handoffs) > 0 {
		// All tool results are preserved in session. The result carries the text the
		// model wrote alongside the handoff calls, not the handoff tools' replies.
		return agentIteration{
			session: session,
			result: &Result{
				Parts: nonBlankParts(textOnly(message.Parts)), Handoffs: handoffs, FinishReason: finish,
			},
		}, nil
	}

	return agentIteration{session: session}, nil
}

// truncatedToolResults returns an error result for each tool call of a
// response cut off at the output limit, without running the calls, and emits
// their ToolResult events when emit is non-nil.
func truncatedToolResults(toolCalls []llm.ToolCall, iteration int, emit emitFunc) []llm.Message {
	results := make([]llm.Message, 0, len(toolCalls))

	for _, tc := range toolCalls {
		results = append(results, llm.Message{
			Role:       llm.RoleTool,
			Parts:      []llm.ContentPart{llm.Text(TruncatedToolCallResult)},
			ToolCallID: tc.ID,
			ToolError:  true,
		})

		if emit != nil {
			emit(Event{
				Type:       EventToolResult,
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
				ToolResult: TruncatedToolCallResult,
				ToolError:  true,
				Iteration:  iteration,
			})
		}
	}

	return results
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

// runToolCalls runs the tool calls of one round and returns their results
// and StopRun errors, in call order. Calls run concurrently, at most
// maxParallelTools at once when set, except exclusive tools (see
// llm.Tool.SetExclusive): each runs alone, after the calls before it and
// before the calls after it. Each result is emitted, on this goroutine, as
// soon as its call finishes.
func (a *Agent) runToolCalls(
	ctx context.Context, toolCalls []llm.ToolCall, iteration int, emit emitFunc,
) ([]*llm.Message, []error) {
	results := make([]*llm.Message, len(toolCalls))
	stops := make([]error, len(toolCalls))

	var batch []int

	for i, tc := range toolCalls {
		if tool, ok := a.getTool(tc.Function.Name); ok && tool.Exclusive() {
			a.runToolBatch(ctx, toolCalls, batch, iteration, emit, results, stops)
			a.runToolBatch(ctx, toolCalls, []int{i}, iteration, emit, results, stops)

			batch = nil

			continue
		}

		batch = append(batch, i)
	}

	a.runToolBatch(ctx, toolCalls, batch, iteration, emit, results, stops)

	return results, stops
}

// runToolBatch runs the tool calls at indexes concurrently, at most
// maxParallelTools at once when set, storing their results and stops.
func (a *Agent) runToolBatch(
	ctx context.Context, toolCalls []llm.ToolCall, indexes []int, iteration int, emit emitFunc,
	results []*llm.Message, stops []error,
) {
	finished := make(chan int, len(indexes))

	var slots chan struct{}
	if a.maxParallelTools > 0 {
		slots = make(chan struct{}, a.maxParallelTools)
	}

	for _, i := range indexes {
		go func() {
			if slots != nil {
				slots <- struct{}{}
				defer func() { <-slots }()
			}

			results[i], stops[i] = a.handleToolCall(ctx, toolCalls[i], iteration)
			finished <- i
		}()
	}

	for range indexes {
		i := <-finished

		if emit == nil {
			continue
		}

		emit(Event{
			Type:       EventToolResult,
			ToolCallID: toolCalls[i].ID,
			ToolName:   toolCalls[i].Function.Name,
			ToolResult: llm.TextContent(results[i].Parts...),
			ToolError:  results[i].ToolError,
			Iteration:  iteration,
		})
	}
}

// prepareSession prepares the messages for the LLM call, including the system prompt, memory messages, and user input.
func (a *Agent) prepareSession(ctx context.Context, rs *runState, parts []llm.ContentPart) ([]llm.Message, int, error) {
	messages := []llm.Message{llm.SystemMessage(a.description)}

	if a.memory != nil {
		snapshot, memoryMessages, err := a.loadMemory(ctx, parts)
		if err != nil {
			return nil, 0, err
		}

		// A stored snapshot starts the session, as after a compaction.
		if snapshot != nil {
			rs.snapshot, rs.stored = snapshot, snapshot
			messages = append(messages, memory.SummaryMessages(*snapshot)...)
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

// loadMemory loads the latest snapshot (nil for none) and the replayable
// messages after it from a memory.SnapshotStore, or the messages from any
// other memory, or from a store set up without snapshot support.
func (a *Agent) loadMemory(ctx context.Context, parts []llm.ContentPart) (*memory.Snapshot, []llm.Message, error) {
	store, ok := a.memory.(memory.SnapshotStore)
	if !ok {
		msgs, err := a.retrieveMemory(ctx, parts)
		return nil, msgs, err
	}

	payload := &trace.Memory{Op: trace.MemoryRetrieve, Query: llm.TextContent(parts...)}
	ctx, span := trace.Start(ctx, string(trace.KindMemory)+" retrieve", payload)

	snapshot, logged, err := store.LoadSnapshot(ctx)
	if errors.Is(err, memory.ErrSnapshotsUnsupported) {
		span.End(nil)

		msgs, retrieveErr := a.retrieveMemory(ctx, parts)

		return nil, msgs, retrieveErr
	}

	if err != nil {
		span.End(err)
		return nil, nil, err
	}

	msgs := make([]llm.Message, 0, len(logged))
	for _, lm := range logged {
		msgs = append(msgs, lm.Message)
	}

	msgs = replayable(msgs)
	payload.Messages = msgs
	payload.Count = len(msgs)

	span.End(nil)

	return snapshot, msgs, nil
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
// A panic in the tool's handler is recovered and returned as a ToolPanicError.
func (a *Agent) executeToolCall(ctx context.Context, tc llm.ToolCall) (parts []llm.ContentPart, err error) {
	tool, found := a.getTool(tc.Function.Name)
	if !found {
		return nil, &ToolUnknownError{Name: tc.Function.Name}
	}

	defer func() {
		if v := recover(); v != nil {
			parts, err = nil, &ToolExecutionError{
				Name: tc.Function.Name,
				Err:  &ToolPanicError{Name: tc.Function.Name, Value: v},
			}
		}
	}()

	result, err := tool.Handle(llm.ContextWithToolCall(ctx, tc), tc.Function.Arguments)
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
		Input string `json:"input" jsonschema_description:"Instructions for the agent. Describe the task, question, or problem the agent should solve."` //nolint:lll
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
