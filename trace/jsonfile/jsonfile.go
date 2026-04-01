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

package jsonfile

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/henomis/phero/trace"
)

// record is the JSON envelope written for each event.
type record struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// Tracer appends one JSON record per line (NDJSON) to a file.
//
// Tracer is safe for concurrent use.
type Tracer struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

// New opens (or creates) the file at filePath in append mode and returns a
// Tracer that writes one JSON line per event.
//
// The caller must call Close when the Tracer is no longer needed in order to
// release the underlying file descriptor.
func New(filePath string) (*Tracer, error) {
	if filePath == "" {
		return nil, ErrEmptyFilePath
	}
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &Tracer{f: f, enc: json.NewEncoder(f)}, nil
}

// Close releases the underlying file descriptor.
func (t *Tracer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.f.Close()
}

// Trace encodes event as a single JSON line and appends it to the file.
func (t *Tracer) Trace(event trace.Event) {
	r := toRecord(event)
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.enc.Encode(r)
}

// toRecord maps a typed Event to a JSON-serialisable record.
func toRecord(event trace.Event) record {
	switch e := event.(type) {
	case trace.AgentStartEvent:
		return record{Type: "AgentStart", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"input":      e.Input,
		}}
	case trace.AgentEndEvent:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		return record{Type: "AgentEnd", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"output":     e.Output,
			"err":        errStr,
			"iterations": e.Iterations,
		}}
	case trace.AgentIterationEvent:
		return record{Type: "AgentIteration", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"iteration":  e.Iteration,
		}}
	case trace.AgentRunSummaryEvent:
		return record{Type: "AgentRunSummary", Timestamp: e.Timestamp, Data: map[string]any{
			"summary": e.Summary,
		}}
	case trace.LLMRequestEvent:
		return record{Type: "LLMRequest", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name":    e.AgentName,
			"message_count": e.MessageCount,
			"tool_names":    e.ToolNames,
			"iteration":     e.Iteration,
		}}
	case trace.LLMResponseEvent:
		return record{Type: "LLMResponse", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"message":    e.Message,
			"usage":      e.Usage,
			"iteration":  e.Iteration,
		}}
	case trace.ToolCallEvent:
		return record{Type: "ToolCall", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"tool_name":  e.ToolName,
			"arguments":  e.Arguments,
			"call_id":    e.CallID,
			"iteration":  e.Iteration,
		}}
	case trace.ToolResultEvent:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		return record{Type: "ToolResult", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"tool_name":  e.ToolName,
			"result":     e.Result,
			"err":        errStr,
			"call_id":    e.CallID,
			"iteration":  e.Iteration,
		}}
	case trace.MemorySaveEvent:
		return record{Type: "MemorySave", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"count":      e.Count,
		}}
	case trace.MemoryRetrieveEvent:
		return record{Type: "MemoryRetrieve", Timestamp: e.Timestamp, Data: map[string]any{
			"agent_name": e.AgentName,
			"count":      e.Count,
		}}
	default:
		return record{Type: "Unknown", Timestamp: time.Now()}
	}
}

// Ensure Tracer implements trace.Tracer at compile time.
var _ trace.Tracer = (*Tracer)(nil)
