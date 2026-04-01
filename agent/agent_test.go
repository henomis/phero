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

package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	openaiapi "github.com/sashabaranov/go-openai"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/trace"
)

// -- mocks -------------------------------------------------------------------

// stubLLM returns responses in the order they are given. Successive calls
// beyond the provided list repeat the last element.
type stubLLM struct {
	responses []*llm.Result
	errs      []error
	callIdx   int
	delay     time.Duration
}

func (s *stubLLM) Execute(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	idx := s.callIdx
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	s.callIdx++
	return s.responses[idx], s.errs[idx]
}

// makeStub builds a stubLLM from alternating (result, error) pairs.
func makeStub(pairs ...any) *stubLLM {
	s := &stubLLM{}
	for i := 0; i+1 < len(pairs); i += 2 {
		var r *llm.Result
		if pairs[i] != nil {
			r = pairs[i].(*llm.Result)
		}
		var e error
		if pairs[i+1] != nil {
			e = pairs[i+1].(error)
		}
		s.responses = append(s.responses, r)
		s.errs = append(s.errs, e)
	}
	return s
}

// textResult builds a successful text-only LLM result.
func textResult(content string) *llm.Result {
	return &llm.Result{
		Message: &llm.Message{
			Role:    llm.ChatMessageRoleAssistant,
			Content: content,
		},
		Usage: &llm.Usage{InputTokens: 10, OutputTokens: 5},
	}
}

// toolCallResult builds an LLM result with a single tool call.
func toolCallResult(toolName, callID, arguments string) *llm.Result {
	return &llm.Result{
		Message: &llm.Message{
			Role: llm.ChatMessageRoleAssistant,
			ToolCalls: []llm.ToolCall{
				{
					ID:   callID,
					Type: llm.ToolTypeFunction,
					Function: openaiapi.FunctionCall{
						Name:      toolName,
						Arguments: arguments,
					},
				},
			},
		},
	}
}

func toolCallResultWithUsage(toolName, callID, arguments string, usage *llm.Usage) *llm.Result {
	result := toolCallResult(toolName, callID, arguments)
	result.Usage = usage
	return result
}

// stubMemory records Save calls and returns pre-configured Retrieve results.
type stubMemory struct {
	retrieved   []llm.Message
	retrieveErr error
	retrieveDur time.Duration
	saved       []llm.Message
	saveErr     error
	saveDur     time.Duration
}

func (m *stubMemory) Retrieve(_ context.Context, _ string) ([]llm.Message, error) {
	if m.retrieveDur > 0 {
		time.Sleep(m.retrieveDur)
	}
	return m.retrieved, m.retrieveErr
}

func (m *stubMemory) Clear(_ context.Context) error { return nil }

func (m *stubMemory) Save(_ context.Context, msgs []llm.Message) error {
	if m.saveDur > 0 {
		time.Sleep(m.saveDur)
	}
	m.saved = msgs
	return m.saveErr
}

type recordingTracer struct {
	events []trace.Event
}

func (t *recordingTracer) Trace(event trace.Event) {
	t.events = append(t.events, event)
}

// -- helpers -----------------------------------------------------------------

func mustNew(t *testing.T, client llm.LLM, name, desc string) *agent.Agent {
	t.Helper()
	a, err := agent.New(client, name, desc)
	if err != nil {
		t.Fatalf("agent.New: unexpected error: %v", err)
	}
	return a
}

func mustTool(t *testing.T, name string, fn func(context.Context, *struct{}) (string, error)) *llm.Tool {
	t.Helper()
	tool, err := llm.NewTool(name, "a test tool", fn)
	if err != nil {
		t.Fatalf("llm.NewTool(%q): unexpected error: %v", name, err)
	}
	return tool
}

// -- tests -------------------------------------------------------------------

func TestNew_Validation(t *testing.T) {
	okLLM := makeStub(textResult("hi"), nil)

	tests := []struct {
		name    string
		client  llm.LLM
		agName  string
		desc    string
		wantErr error
	}{
		{
			name:    "nil LLM",
			client:  nil,
			agName:  "agent",
			desc:    "desc",
			wantErr: agent.ErrUndefinedLLM,
		},
		{
			name:    "empty name",
			client:  okLLM,
			agName:  "",
			desc:    "desc",
			wantErr: agent.ErrNameRequired,
		},
		{
			name:    "empty description",
			client:  okLLM,
			agName:  "agent",
			desc:    "",
			wantErr: agent.ErrDescriptionRequired,
		},
		{
			name:   "valid",
			client: okLLM,
			agName: "agent",
			desc:   "a helpful agent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := agent.New(tc.client, tc.agName, tc.desc)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want error %v, got %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAddTool_DuplicateReturnsError(t *testing.T) {
	a := mustNew(t, makeStub(textResult("hi"), nil), "agent", "desc")

	tool := mustTool(t, "my_tool", func(_ context.Context, _ *struct{}) (string, error) {
		return "ok", nil
	})

	if err := a.AddTool(tool); err != nil {
		t.Fatalf("first AddTool: unexpected error: %v", err)
	}

	err := a.AddTool(tool)
	var alreadyExists *agent.ToolAlreadyExistsError
	if !errors.As(err, &alreadyExists) {
		t.Fatalf("expected ToolAlreadyExistsError, got %v", err)
	}
	if alreadyExists.Name != "my_tool" {
		t.Fatalf("expected error for tool %q, got %q", "my_tool", alreadyExists.Name)
	}
}

func TestAddHandoff_DuplicateReturnsError(t *testing.T) {
	a := mustNew(t, makeStub(textResult("hi"), nil), "orchestrator", "orchestrates")
	target := mustNew(t, makeStub(textResult("done"), nil), "worker", "does work")

	if err := a.AddHandoff(target); err != nil {
		t.Fatalf("first AddHandoff: unexpected error: %v", err)
	}

	err := a.AddHandoff(target)
	var alreadyExists *agent.ToolAlreadyExistsError
	if !errors.As(err, &alreadyExists) {
		t.Fatalf("expected ToolAlreadyExistsError on second AddHandoff, got %v", err)
	}
}

func TestRun_Simple(t *testing.T) {
	stub := makeStub(textResult("Hello, world!"), nil)
	a := mustNew(t, stub, "agent", "a helpful agent")

	result, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Content != "Hello, world!" {
		t.Fatalf("expected %q, got %q", "Hello, world!", result.Content)
	}
	if result.HandoffAgent != nil {
		t.Fatalf("expected no handoff agent, got %v", result.HandoffAgent)
	}
}

func TestRun_LLMError(t *testing.T) {
	stub := makeStub(nil, errors.New("model unavailable"))
	a := mustNew(t, stub, "agent", "a helpful agent")

	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRun_MaxIterations(t *testing.T) {
	// LLM always returns a tool call referencing a missing tool; the agent
	// converts the unknown-tool error to an error message and iterates again.
	// With maxIterations=2 it must eventually return ErrMaxIterationsReached.
	toolCallRes := toolCallResult("missing_tool", "id-1", "{}")
	stub := &stubLLM{
		responses: []*llm.Result{toolCallRes, toolCallRes, toolCallRes, toolCallRes},
		errs:      []error{nil, nil, nil, nil},
	}
	a := mustNew(t, stub, "agent", "desc")
	a.SetMaxIterations(2)

	_, err := a.Run(context.Background(), "go")
	if !errors.Is(err, agent.ErrMaxIterationsReached) {
		t.Fatalf("expected ErrMaxIterationsReached, got %v", err)
	}
}

func TestRun_ToolCall_Success(t *testing.T) {
	toolInvoked := false

	tool := mustTool(t, "echo_tool", func(_ context.Context, _ *struct{}) (string, error) {
		toolInvoked = true
		return "echo_result", nil
	})

	// First call: tool call. Second call: final text.
	stub := &stubLLM{
		responses: []*llm.Result{
			toolCallResult("echo_tool", "call-1", "{}"),
			textResult("done"),
		},
		errs: []error{nil, nil},
	}

	a := mustNew(t, stub, "agent", "desc")
	if err := a.AddTool(tool); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, err := a.Run(context.Background(), "do it")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if !toolInvoked {
		t.Fatal("expected tool to be invoked")
	}
	if result.Content != "done" {
		t.Fatalf("expected %q, got %q", "done", result.Content)
	}
}

func TestRun_ToolCall_ToolErrors_AgentContinues(t *testing.T) {
	// A tool that errors produces an error message in the conversation rather
	// than terminating the agent loop.
	errTool := mustTool(t, "fail_tool", func(_ context.Context, _ *struct{}) (string, error) {
		return "", errors.New("tool broke")
	})

	stub := &stubLLM{
		responses: []*llm.Result{
			toolCallResult("fail_tool", "call-1", "{}"),
			textResult("recovered"),
		},
		errs: []error{nil, nil},
	}

	a := mustNew(t, stub, "agent", "desc")
	if err := a.AddTool(errTool); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, err := a.Run(context.Background(), "break it")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Content != "recovered" {
		t.Fatalf("expected %q, got %q", "recovered", result.Content)
	}
}

func TestRun_Memory_ReadAndWrite(t *testing.T) {
	prevMessages := []llm.Message{
		{Role: llm.ChatMessageRoleUser, Content: "old message"},
		{Role: llm.ChatMessageRoleAssistant, Content: "old reply"},
	}
	mem := &stubMemory{retrieved: prevMessages}

	stub := makeStub(textResult("new reply"), nil)
	a := mustNew(t, stub, "agent", "desc")
	a.SetMemory(mem)

	_, err := a.Run(context.Background(), "new message")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	// memory.Save must have been called with at least the new exchange.
	if len(mem.saved) == 0 {
		t.Fatal("expected memory.Save to be called with messages")
	}
}

func TestRun_Memory_RetrieveError(t *testing.T) {
	mem := &stubMemory{retrieveErr: errors.New("retrieve failed")}

	stub := makeStub(textResult("hi"), nil)
	a := mustNew(t, stub, "agent", "desc")
	a.SetMemory(mem)

	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error from memory.Retrieve, got nil")
	}
}

func TestRun_Memory_SaveError_ResultStillReturned(t *testing.T) {
	mem := &stubMemory{saveErr: errors.New("save failed")}

	stub := makeStub(textResult("answer"), nil)
	a := mustNew(t, stub, "agent", "desc")
	a.SetMemory(mem)

	result, err := a.Run(context.Background(), "hi")
	// Run must return both the result and the joined save error.
	if result == nil {
		t.Fatal("expected result even when save fails")
	}
	if result.Content != "answer" {
		t.Fatalf("expected %q, got %q", "answer", result.Content)
	}
	if err == nil {
		t.Fatal("expected save error to be surfaced")
	}
	if !errors.Is(err, agent.ErrSessionSaveFailed) {
		t.Fatalf("expected ErrSessionSaveFailed in error chain, got %v", err)
	}
}

func TestRun_Handoff(t *testing.T) {
	worker := mustNew(t, makeStub(textResult("worker done"), nil), "worker", "does work")

	// First call returns a handoff tool call; the agent should set HandoffAgent.
	orchestrator := mustNew(t,
		makeStub(
			toolCallResult("handoff_to_worker", "h-1", `{"context":"go work"}`),
			nil,
		),
		"orchestrator", "orchestrates",
	)

	if err := orchestrator.AddHandoff(worker); err != nil {
		t.Fatalf("AddHandoff: %v", err)
	}

	result, err := orchestrator.Run(context.Background(), "delegate")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.HandoffAgent == nil {
		t.Fatal("expected HandoffAgent to be set")
	}
	if result.HandoffAgent.Name() != "worker" {
		t.Fatalf("expected handoff to %q, got %q", "worker", result.HandoffAgent.Name())
	}
}

func TestRun_PopulatesRunSummary(t *testing.T) {
	mem := &stubMemory{
		retrieved: []llm.Message{
			{Role: llm.ChatMessageRoleUser, Content: "previous question"},
			{Role: llm.ChatMessageRoleAssistant, Content: "previous answer"},
		},
		retrieveDur: time.Millisecond,
		saveDur:     time.Millisecond,
	}
	stub := &stubLLM{
		responses: []*llm.Result{
			toolCallResultWithUsage("echo_tool", "call-1", "{}", &llm.Usage{InputTokens: 7, OutputTokens: 3}),
			textResult("done"),
		},
		errs:  []error{nil, nil},
		delay: time.Millisecond,
	}
	tracer := &recordingTracer{}

	a := mustNew(t, stub, "agent", "desc")
	a.SetMemory(mem)
	a.SetTracer(tracer)
	if err := a.AddTool(mustTool(t, "echo_tool", func(_ context.Context, _ *struct{}) (string, error) {
		time.Sleep(time.Millisecond)
		return "echo_result", nil
	})); err != nil {
		t.Fatalf("AddTool: %v", err)
	}

	result, err := a.Run(context.Background(), "do it")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Summary == nil {
		t.Fatal("expected run summary on result")
	}

	summary := result.Summary
	if summary.AgentName != "agent" {
		t.Fatalf("summary.AgentName = %q, want %q", summary.AgentName, "agent")
	}
	if summary.Iterations != 2 {
		t.Fatalf("summary.Iterations = %d, want 2", summary.Iterations)
	}
	if summary.LLMCalls != 2 {
		t.Fatalf("summary.LLMCalls = %d, want 2", summary.LLMCalls)
	}
	if summary.ToolCalls != 1 {
		t.Fatalf("summary.ToolCalls = %d, want 1", summary.ToolCalls)
	}
	if summary.ToolErrors != 0 {
		t.Fatalf("summary.ToolErrors = %d, want 0", summary.ToolErrors)
	}
	if summary.MemoryRetrieved != 2 {
		t.Fatalf("summary.MemoryRetrieved = %d, want 2", summary.MemoryRetrieved)
	}
	if summary.MemorySaved != 4 {
		t.Fatalf("summary.MemorySaved = %d, want 4", summary.MemorySaved)
	}
	if summary.Usage.InputTokens != 17 {
		t.Fatalf("summary.Usage.InputTokens = %d, want 17", summary.Usage.InputTokens)
	}
	if summary.Usage.OutputTokens != 8 {
		t.Fatalf("summary.Usage.OutputTokens = %d, want 8", summary.Usage.OutputTokens)
	}
	if len(summary.Tools) != 1 {
		t.Fatalf("len(summary.Tools) = %d, want 1", len(summary.Tools))
	}
	if summary.Tools[0].ToolName != "echo_tool" || summary.Tools[0].Calls != 1 || summary.Tools[0].Errors != 0 {
		t.Fatalf("unexpected tool summary: %+v", summary.Tools[0])
	}
	if summary.Latency.Total <= 0 || summary.Latency.LLM <= 0 || summary.Latency.Tool <= 0 || summary.Latency.Memory <= 0 {
		t.Fatalf("expected positive latency metrics, got %+v", summary.Latency)
	}

	var eventSummary *trace.RunSummary
	for _, event := range tracer.events {
		runSummaryEvent, ok := event.(trace.AgentRunSummaryEvent)
		if ok {
			eventSummary = &runSummaryEvent.Summary
			break
		}
	}
	if eventSummary == nil {
		t.Fatal("expected AgentRunSummaryEvent to be emitted")
	}
	if eventSummary.ToolCalls != summary.ToolCalls {
		t.Fatalf("event summary tool calls = %d, want %d", eventSummary.ToolCalls, summary.ToolCalls)
	}
}

func TestRun_EmptyInput(t *testing.T) {
	stub := makeStub(textResult("what can I help with?"), nil)
	a := mustNew(t, stub, "agent", "desc")

	result, err := a.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Content == "" {
		t.Fatal("expected non-empty result")
	}
}
