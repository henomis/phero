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

package permission_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/tool/permission"
)

func bashArgs(command string) string {
	b, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		panic(err)
	}

	return string(b)
}

func mustPolicy(t *testing.T, opts ...permission.Option) *permission.Policy {
	t.Helper()

	p, err := permission.New(opts...)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestNew_InvalidRule(t *testing.T) {
	for _, spec := range []string{"bash(go test", "(x)", ""} {
		if _, err := permission.New(permission.WithAllow(spec)); !errors.Is(err, permission.ErrInvalidRule) {
			t.Errorf("rule %q: err = %v, want ErrInvalidRule", spec, err)
		}
	}
}

func TestDecide(t *testing.T) {
	p := mustPolicy(t,
		permission.WithAllow("read", "bash(go test *)", "bash(go build *)", "edit(/repo/*)", "mcp_github_*"),
		permission.WithAsk("edit(/repo/go.mod)"),
		permission.WithDeny("bash(rm -rf *)", "edit(/repo/secrets/*)"),
	)

	cases := []struct {
		tool, args string
		want       permission.Decision
	}{
		{"read", `{"file_path":"/etc/passwd"}`, permission.Allow},
		{"bash", bashArgs("go test ./..."), permission.Allow},
		{"bash", bashArgs("go test ./... && go build ./cmd"), permission.Allow},
		{"bash", bashArgs("go test ./... && rm -rf /"), permission.Deny},
		{"bash", bashArgs("go test ./... | tee out.txt"), permission.Ask},
		{"bash", bashArgs("go test $(rm -rf /)"), permission.Ask},
		{"bash", bashArgs("go test `whoami`"), permission.Ask},
		{"bash", bashArgs("ls"), permission.Ask},
		{"edit", `{"file_path":"/repo/main.go"}`, permission.Allow},
		{"edit", `{"file_path":"/repo/sub/deep.go"}`, permission.Allow}, // * matches slashes
		{"edit", `{"file_path":"/repo/go.mod"}`, permission.Ask},        // ask wins over allow
		{"edit", `{"file_path":"/repo/secrets/key.pem"}`, permission.Deny},
		{"edit", `{"file_path":"/other/main.go"}`, permission.Ask},
		{"mcp_github_create_issue", `{}`, permission.Allow},
		{"write", `{"file_path":"/repo/x"}`, permission.Ask},
	}

	for _, tc := range cases {
		if got, _ := p.Decide(tc.tool, tc.args); got != tc.want {
			t.Errorf("Decide(%s, %s) = %s, want %s", tc.tool, tc.args, got, tc.want)
		}
	}
}

func TestDecide_DefaultAndSubject(t *testing.T) {
	p := mustPolicy(t,
		permission.WithDefault(permission.Deny),
		permission.WithSubject("fetch", "url"),
		permission.WithAllow("fetch(https://go.dev/*)"),
	)

	if got, _ := p.Decide("fetch", `{"url":"https://go.dev/doc"}`); got != permission.Allow {
		t.Errorf("allowed url = %s", got)
	}

	if got, _ := p.Decide("fetch", `{"url":"https://evil.example"}`); got != permission.Deny {
		t.Errorf("other url = %s, want the default", got)
	}
}

// newBashTool returns a tool named bash that counts its runs.
func newBashTool(t *testing.T, runs *atomic.Int32) *llm.Tool {
	t.Helper()

	type input struct {
		Command string `json:"command"`
	}

	tool, err := llm.NewTool("bash", "test tool", func(context.Context, *input) (string, error) {
		runs.Add(1)
		return "ran", nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return tool
}

func TestMiddleware_AskAndAllowAlways(t *testing.T) {
	var asked atomic.Int32

	p := mustPolicy(t, permission.WithAsker(func(_ context.Context, req permission.Request) (permission.Answer, error) {
		asked.Add(1)

		if req.Subject == "make lint" {
			return permission.Reject, nil
		}

		return permission.AllowAlways, nil
	}))

	var runs atomic.Int32

	tool := newBashTool(t, &runs)
	p.Apply(tool)

	for range 2 {
		if _, err := tool.Handle(context.Background(), bashArgs("make test")); err != nil {
			t.Fatal(err)
		}
	}

	if asked.Load() != 1 || runs.Load() != 2 {
		t.Fatalf("asked %d times, ran %d times; want 1 and 2 (AllowAlways remembered)", asked.Load(), runs.Load())
	}

	_, err := tool.Handle(context.Background(), bashArgs("make lint"))

	var denied *permission.DeniedError
	if !errors.As(err, &denied) || !errors.Is(err, permission.ErrDenied) || denied.Subject != "make lint" {
		t.Fatalf("err = %v, want a DeniedError for make lint", err)
	}
}

func TestMiddleware_NoAskerDenies(t *testing.T) {
	var runs atomic.Int32

	tool := newBashTool(t, &runs)
	mustPolicy(t).Apply(tool)

	if _, err := tool.Handle(context.Background(), bashArgs("ls")); !errors.Is(err, permission.ErrDenied) || runs.Load() != 0 {
		t.Fatalf("err = %v, runs = %d; want denied without running", err, runs.Load())
	}
}

func TestMiddleware_AsksOneAtATime(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32

	p := mustPolicy(t, permission.WithAsker(func(context.Context, permission.Request) (permission.Answer, error) {
		n := inFlight.Add(1)
		if n > maxInFlight.Load() {
			maxInFlight.Store(n)
		}

		time.Sleep(10 * time.Millisecond)
		inFlight.Add(-1)

		return permission.AllowOnce, nil
	}))

	var runs atomic.Int32

	tool := newBashTool(t, &runs)
	p.Apply(tool)

	var wg sync.WaitGroup

	for i := range 8 {
		wg.Go(func() {
			if _, err := tool.Handle(context.Background(), bashArgs("cmd "+string(rune('a'+i)))); err != nil {
				t.Error(err)
			}
		})
	}

	wg.Wait()

	if maxInFlight.Load() != 1 || runs.Load() != 8 {
		t.Fatalf("max questions at once = %d, runs = %d; want 1 and 8", maxInFlight.Load(), runs.Load())
	}
}

// scriptedLLM calls bash once, then answers with the text of the tool result.
type scriptedLLM struct{ calls int }

func (s *scriptedLLM) Execute(_ context.Context, msgs []llm.Message, _ ...llm.CallOption) (*llm.Result, error) {
	s.calls++

	if s.calls == 1 {
		msg := llm.AssistantMessage(nil, llm.ToolCall{
			ID: "call-7", Type: llm.ToolTypeFunction, Function: llm.FunctionCall{Name: "bash", Arguments: bashArgs("rm -rf /")},
		})

		return &llm.Result{Message: &msg}, nil
	}

	last := msgs[len(msgs)-1]
	msg := llm.AssistantMessage([]llm.ContentPart{llm.Text(last.TextContent())})

	return &llm.Result{Message: &msg}, nil
}

func TestPolicy_DenialReachesTheModel(t *testing.T) {
	var (
		runs   atomic.Int32
		callID string
	)

	tool := newBashTool(t, &runs)
	mustPolicy(t, permission.WithAsker(func(_ context.Context, req permission.Request) (permission.Answer, error) {
		callID = req.CallID
		return permission.Reject, nil
	})).Apply(tool)

	a, err := agent.New(&scriptedLLM{}, "coder", "You write code.")
	if err != nil {
		t.Fatal(err)
	}

	if err = a.AddTool(tool); err != nil {
		t.Fatal(err)
	}

	res, err := a.Run(context.Background(), llm.Text("clean up"))
	if err != nil {
		t.Fatal(err)
	}

	if runs.Load() != 0 || callID != "call-7" || res.Summary.ToolErrors != 1 {
		t.Fatalf("runs = %d, call ID = %q, tool errors = %d; want 0, call-7, 1", runs.Load(), callID, res.Summary.ToolErrors)
	}

	if got := res.TextContent(); !strings.Contains(got, "permission denied for bash(rm -rf /): denied by the user") {
		t.Fatalf("model saw %q, want the denial", got)
	}
}

func TestPolicy_AllowAtRuntimeAndForeignSyntax(t *testing.T) {
	p := mustPolicy(t)

	if got, _ := p.Decide("bash", bashArgs("git status")); got != permission.Ask {
		t.Fatalf("before Allow: %s", got)
	}

	// The form skills use in allowed-tools.
	if err := p.Allow("Bash(git:*)", "Read"); err != nil {
		t.Fatal(err)
	}

	if got, _ := p.Decide("bash", bashArgs("git status && git diff")); got != permission.Allow {
		t.Fatalf("after Allow: bash = %s", got)
	}

	if got, _ := p.Decide("read", `{"file_path":"/x"}`); got != permission.Allow {
		t.Fatalf("after Allow: read = %s", got)
	}

	if err := p.Allow("bash(oops"); !errors.Is(err, permission.ErrInvalidRule) {
		t.Fatalf("err = %v, want ErrInvalidRule", err)
	}
}
