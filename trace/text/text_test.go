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

package text_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/trace"
	"github.com/henomis/phero/trace/text"
)

func TestTextTracer_AgentStart(t *testing.T) {
	var buf bytes.Buffer
	tr := text.New(&buf)
	tr.Trace(trace.AgentStartEvent{
		AgentName: "myagent",
		Input:     "hello",
		Timestamp: time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC),
	})
	out := buf.String()
	if !strings.Contains(out, "AgentStart") {
		t.Errorf("expected 'AgentStart' in output, got: %q", out)
	}
	if !strings.Contains(out, "myagent") {
		t.Errorf("expected agent name in output, got: %q", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected input in output, got: %q", out)
	}
}

func TestTextTracer_ToolCallResult(t *testing.T) {
	var buf bytes.Buffer
	tr := text.New(&buf)
	tr.Trace(trace.ToolCallEvent{
		AgentName: "agent1", ToolName: "bash",
		Arguments: `{"cmd":"ls"}`, Iteration: 1,
		Timestamp: time.Now(),
	})
	tr.Trace(trace.ToolResultEvent{
		AgentName: "agent1", ToolName: "bash",
		Result: "file.txt\n", Iteration: 1,
		Timestamp: time.Now(),
	})
	out := buf.String()
	if !strings.Contains(out, "ToolCall") {
		t.Errorf("expected 'ToolCall' in output")
	}
	if !strings.Contains(out, "ToolResult") {
		t.Errorf("expected 'ToolResult' in output")
	}
	if !strings.Contains(out, "✓") {
		t.Errorf("expected success icon in output")
	}
}

func TestTextTracer_ToolResultError(t *testing.T) {
	var buf bytes.Buffer
	tr := text.New(&buf)
	tr.Trace(trace.ToolResultEvent{
		AgentName: "agent1", ToolName: "bash",
		Err: errTest, Iteration: 1,
		Timestamp: time.Now(),
	})
	out := buf.String()
	if !strings.Contains(out, "✗") {
		t.Errorf("expected error icon in output, got: %q", out)
	}
}

func TestTextTracer_AllEventTypes_NoPanic(t *testing.T) {
	var buf bytes.Buffer
	tr := text.New(&buf)
	now := time.Now()
	events := []trace.Event{
		trace.AgentStartEvent{AgentName: "a", Input: "i", Timestamp: now},
		trace.AgentEndEvent{AgentName: "a", Output: "o", Iterations: 1, Timestamp: now},
		trace.AgentIterationEvent{AgentName: "a", Iteration: 1, Timestamp: now},
		trace.LLMRequestEvent{AgentName: "a", MessageCount: 2, ToolNames: []string{"t1"}, Iteration: 1, Timestamp: now},
		trace.LLMResponseEvent{AgentName: "a", Iteration: 1, Timestamp: now},
		trace.ToolCallEvent{AgentName: "a", ToolName: "t", Arguments: "{}", Iteration: 1, Timestamp: now},
		trace.ToolResultEvent{AgentName: "a", ToolName: "t", Result: "ok", Iteration: 1, Timestamp: now},
		trace.MemorySaveEvent{AgentName: "a", Count: 3, Timestamp: now},
		trace.MemoryRetrieveEvent{AgentName: "a", Count: 2, Timestamp: now},
	}
	for _, e := range events {
		tr.Trace(e)
	}
	if buf.Len() == 0 {
		t.Error("expected non-empty output")
	}
}

// helpers

type testError struct{ msg string }

func (e testError) Error() string { return e.msg }

var errTest = testError{"tool failed"}
