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

package a2a_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	pheroA2A "github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// stubLLM returns responses in the order they are given. The last element
// is repeated for any call beyond the provided list.
type stubLLM struct {
	responses []*llm.Result
	errs      []error
	callIdx   int
}

func (s *stubLLM) Execute(_ context.Context, _ []llm.Message, _ []*llm.Tool) (*llm.Result, error) {
	idx := s.callIdx
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	s.callIdx++
	return s.responses[idx], s.errs[idx]
}

// textLLM returns a stub that always returns the given text.
func textLLM(text string) *stubLLM {
	return &stubLLM{
		responses: []*llm.Result{
			{Message: &llm.Message{Role: llm.RoleAssistant, Parts: []llm.ContentPart{llm.Text(text)}}},
		},
		errs: []error{nil},
	}
}

// errLLM returns a stub that always returns the given error.
func errLLM(err error) *stubLLM {
	return &stubLLM{
		responses: []*llm.Result{nil},
		errs:      []error{err},
	}
}

// mustAgent creates an *agent.Agent with the given stub LLM.
func mustAgent(t *testing.T, stub llm.LLM, name, desc string) *agent.Agent {
	t.Helper()
	ag, err := agent.New(stub, name, desc)
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}
	return ag
}

// newTestServer spins up an httptest.Server with the a2a.Server mounted on it
// and returns both the httptest.Server and the a2a.Server.
func newTestServer(t *testing.T, stub llm.LLM, opts ...pheroA2A.ServerOption) (*httptest.Server, *pheroA2A.Server) {
	t.Helper()
	ag := mustAgent(t, stub, "test-agent", "A test agent.")
	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	srv, err := pheroA2A.New(ag, ts.URL, opts...)
	if err != nil {
		t.Fatalf("a2a.New: %v", err)
	}
	srv.Mount(mux)
	return ts, srv
}
