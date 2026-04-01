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

package jsonfile_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/trace"
	"github.com/henomis/phero/trace/jsonfile"
)

func TestNew_EmptyPath_ReturnsError(t *testing.T) {
	_, err := jsonfile.New("")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestNew_ValidPath_CreatesFile(t *testing.T) {
	f, err := os.CreateTemp("", "trace-*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	removeIfExists(t, path)
	t.Cleanup(func() {
		removeIfExists(t, path)
	})

	tr, err := jsonfile.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to be created")
	}
}

func TestTracer_Trace_WritesNDJSON(t *testing.T) {
	f, err := os.CreateTemp("", "trace-*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Cleanup(func() {
		removeIfExists(t, path)
	})

	tr, err := jsonfile.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	now := time.Now()
	events := []trace.Event{
		trace.AgentStartEvent{AgentName: "a", Input: "hello", Timestamp: now},
		trace.AgentIterationEvent{AgentName: "a", Iteration: 1, Timestamp: now},
		trace.AgentRunSummaryEvent{Summary: trace.RunSummary{AgentName: "a"}, Timestamp: now},
		trace.AgentEndEvent{AgentName: "a", Output: "ok", Iterations: 2, Timestamp: now},
		trace.LLMRequestEvent{AgentName: "a", MessageCount: 3, ToolNames: []string{"t1"}, Iteration: 1, Timestamp: now},
		trace.LLMResponseEvent{AgentName: "a", Iteration: 1, Timestamp: now},
		trace.ToolCallEvent{AgentName: "a", ToolName: "bash", Arguments: "{}", CallID: "c1", Iteration: 1, Timestamp: now},
		trace.ToolResultEvent{AgentName: "a", ToolName: "bash", Result: "ok", CallID: "c1", Iteration: 1, Timestamp: now},
		trace.MemorySaveEvent{AgentName: "a", Count: 4, Timestamp: now},
		trace.MemoryRetrieveEvent{AgentName: "a", Count: 2, Timestamp: now},
	}
	for _, e := range events {
		tr.Trace(e)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	expectedTypes := []string{
		"AgentStart", "AgentIteration", "AgentRunSummary", "AgentEnd",
		"LLMRequest", "LLMResponse",
		"ToolCall", "ToolResult",
		"MemorySave", "MemoryRetrieve",
	}

	scanner := bufio.NewScanner(file)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	if len(lines) != len(expectedTypes) {
		t.Fatalf("expected %d lines, got %d", len(expectedTypes), len(lines))
	}

	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("line %d is not valid JSON: %v - %q", i, err, line)
			continue
		}
		got, _ := m["type"].(string)
		if !strings.EqualFold(got, expectedTypes[i]) {
			t.Errorf("line %d: expected type %q, got %q", i, expectedTypes[i], got)
		}
	}
}

func TestTracer_Trace_ErrorEvent(t *testing.T) {
	f, err := os.CreateTemp("", "trace-*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	t.Cleanup(func() {
		removeIfExists(t, path)
	})

	tr, err := jsonfile.New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tr.Trace(trace.AgentEndEvent{AgentName: "a", Err: errTest, Iterations: 1, Timestamp: time.Now()})
	tr.Trace(trace.ToolResultEvent{AgentName: "a", ToolName: "t", Err: errTest, Iteration: 1, Timestamp: time.Now()})
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "tool failed") {
		t.Errorf("expected error message in output, got: %s", data)
	}
}

// helpers

type testError struct{ msg string }

func (e testError) Error() string { return e.msg }

var errTest = testError{"tool failed"}

func removeIfExists(t *testing.T, path string) {
	t.Helper()

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Remove(%q): %v", path, err)
	}
}
