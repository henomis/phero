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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/trace"
	"github.com/henomis/phero/v2/trace/text"
)

func TestProcessor_PrintsIndentedTree(t *testing.T) {
	var buf bytes.Buffer

	p := trace.NewProvider(trace.WithProcessor(text.New(&buf)))
	ctx := trace.WithProvider(context.Background(), p)

	ctx, run := trace.Start(ctx, "invoke_agent myagent", &trace.Run{Agent: "myagent", Input: []llm.ContentPart{llm.Text("hello")}})
	tctx, tool := trace.Start(ctx, "execute_tool bash", &trace.ToolCall{Name: "bash", Arguments: `{"cmd":"ls"}`, Result: "file.txt"})
	tool.AddEvent("guardrail.blocked", trace.String("phero.guardrail.name", "pii"))
	tool.End(errors.New("permission denied"))

	_, call := trace.Start(tctx, "chat gpt", &trace.LLMCall{ResponseModel: "gpt", Usage: llm.Usage{InputTokens: 3, OutputTokens: 4}})
	call.End(nil)

	run.End(nil)

	out := buf.String()
	for _, want := range []string{
		"▶", "invoke_agent myagent", "input=hello",
		"\n" + "\033[2m", "  ", "execute_tool bash", `args={"cmd":"ls"}`, "result=file.txt", "err=", "permission denied",
		"guardrail.blocked", "name=pii", "stop=", "tokens=3/4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d:\n%s", len(lines), out)
	}
}

func TestProcessor_MaxLen(t *testing.T) {
	var buf bytes.Buffer

	p := trace.NewProvider(trace.WithProcessor(text.New(&buf, text.WithMaxLen(3), text.WithVerbose())))
	_, s := trace.Start(trace.WithProvider(context.Background(), p), "t", &trace.ToolCall{Name: "t", Arguments: "abcdef"})
	s.End(nil)

	if !strings.Contains(buf.String(), "args=abc…") {
		t.Fatalf("expected truncated args, got %q", buf.String())
	}
}
