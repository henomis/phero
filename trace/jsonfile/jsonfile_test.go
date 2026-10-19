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
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/jsonfile"
)

func TestNew_EmptyPath_ReturnsError(t *testing.T) {
	if _, err := jsonfile.New(""); !errors.Is(err, jsonfile.ErrEmptyFilePath) {
		t.Fatalf("expected ErrEmptyFilePath, got %v", err)
	}
}

func TestProcessor_WritesOneLinePerSpan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.ndjson")

	proc, err := jsonfile.New(path)
	if err != nil {
		t.Fatal(err)
	}

	p := trace.NewProvider(trace.WithProcessor(proc))
	ctx := trace.WithProvider(context.Background(), p)

	ctx, run := trace.Start(ctx, "invoke_agent a", &trace.Run{Agent: "a"})
	_, call := trace.Start(ctx, "chat m", &trace.LLMCall{
		ResponseModel: "m",
		Messages:      []llm.Message{llm.UserMessage(llm.Text("hello"))},
		Usage:         llm.Usage{InputTokens: 2},
	})
	call.End(errors.New("boom"))
	run.End(nil)

	if err = p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path) //nolint:gosec // test file
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var records []map[string]any

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r map[string]any
		if err = json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("invalid json line: %v", err)
		}

		records = append(records, r)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	llmRec, runRec := records[0], records[1]
	if llmRec["kind"] != "chat" || llmRec["error"] != "boom" || llmRec["parentSpanId"] != runRec["spanId"] {
		t.Fatalf("unexpected llm record %v", llmRec)
	}

	payload, _ := llmRec["payload"].(map[string]any)
	if msgs, _ := payload["messages"].([]any); len(msgs) != 1 {
		t.Fatalf("expected full prompt in payload, got %v", payload)
	}

	totals, _ := runRec["totals"].(map[string]any)
	if totals["llmCalls"] != float64(1) {
		t.Fatalf("expected run totals, got %v", runRec["totals"])
	}
}
