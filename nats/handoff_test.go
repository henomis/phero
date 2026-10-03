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

package nats

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// resultHandler answers every prompt with a fixed result.
type resultHandler struct{ result *agent.Result }

func (h resultHandler) Run(context.Context, ...llm.ContentPart) (*agent.Result, error) {
	return h.result, nil
}

func newNamedAgent(t *testing.T, name string) *agent.Agent {
	t.Helper()

	noLLM := llm.Func(func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
		return nil, nil
	})

	a, err := agent.New(noLLM, name, "a handoff target")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	return a
}

// serveResult runs one prompt against a handler returning result, logging to
// a buffer, and returns the log lines and what the caller read.
func serveResult(t *testing.T, result *agent.Result) ([]map[string]any, string) {
	t.Helper()

	var logs bytes.Buffer

	s, ctx := newTestServer(t, time.Second)
	s.handler = resultHandler{result: result}
	WithLogger(slog.New(slog.NewJSONHandler(&logs, nil)))(s.cfg)

	req := &fakeRequest{data: []byte("hi")}
	s.processPrompt(ctx, req)

	var lines []map[string]any

	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("log line is not JSON: %v (%s)", err, line)
		}

		lines = append(lines, entry)
	}

	return lines, responseText(t, req.responses)
}

// TestProcessPrompt_WarnsOnDroppedHandoffs is issue #9: handoffs cannot cross
// the wire, so the server says so instead of dropping them silently, and still
// sends the text.
func TestProcessPrompt_WarnsOnDroppedHandoffs(t *testing.T) {
	result := &agent.Result{
		Parts: []llm.ContentPart{llm.Text("routing you")},
		Handoffs: []agent.Handoff{
			{Agent: newNamedAgent(t, "billing")},
			{Agent: newNamedAgent(t, "refunds")},
		},
	}

	logs, text := serveResult(t, result)

	if text != "routing you" {
		t.Fatalf("caller read %q, want the text still sent", text)
	}

	if len(logs) != 1 || logs[0]["level"] != "WARN" {
		t.Fatalf("logs = %v, want one warning", logs)
	}

	handoffs, _ := logs[0]["handoffs"].([]any)
	if len(handoffs) != 2 || handoffs[0] != "billing" || handoffs[1] != "refunds" {
		t.Fatalf("handoffs logged = %v, want [billing refunds]", logs[0]["handoffs"])
	}

	if logs[0]["owner"] != "acme" || logs[0]["name"] != "worker" {
		t.Fatalf("log = %v, want the agent's owner and name", logs[0])
	}
}

// TestProcessPrompt_HandoffWithoutText sends a note naming the target and its
// context instead of an empty answer, which would read like an error.
func TestProcessPrompt_HandoffWithoutText(t *testing.T) {
	_, text := serveResult(t, &agent.Result{
		Handoffs: []agent.Handoff{{Agent: newNamedAgent(t, "billing"), Context: "invoice 42 is wrong"}},
	})

	if want := "handed off to billing: invoice 42 is wrong"; text != want {
		t.Fatalf("caller read %q, want %q", text, want)
	}
}

func TestProcessPrompt_NoHandoffsNoWarning(t *testing.T) {
	logs, text := serveResult(t, &agent.Result{Parts: []llm.ContentPart{llm.Text("done")}})

	if text != "done" || len(logs) != 0 {
		t.Fatalf("text = %q, logs = %v; want the text and no log", text, logs)
	}
}

// TestProcessPrompt_DefaultLogger verifies that a server without WithLogger
// warns through slog.Default() rather than failing.
func TestProcessPrompt_DefaultLogger(t *testing.T) {
	var logs bytes.Buffer

	previous := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	s, ctx := newTestServer(t, time.Second)
	s.handler = resultHandler{result: &agent.Result{
		Parts:    []llm.ContentPart{llm.Text("ok")},
		Handoffs: []agent.Handoff{{Agent: newNamedAgent(t, "billing")}},
	}}

	s.processPrompt(ctx, &fakeRequest{data: []byte("hi")})

	if !bytes.Contains(logs.Bytes(), []byte("handoffs dropped")) {
		t.Fatalf("default logger got %q, want the warning", logs.String())
	}
}
