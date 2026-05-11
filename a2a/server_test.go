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
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	pheroA2A "github.com/henomis/phero/a2a"
	"github.com/henomis/phero/agent"
)

func TestNew_Validation(t *testing.T) {
	stub := textLLM("hi")
	ag := mustAgent(t, stub, "ag", "desc")

	tests := []struct {
		name    string
		agent   *agent.Agent
		baseURL string
		wantErr error
	}{
		{"nil agent", nil, "http://localhost:8080", pheroA2A.ErrAgentRequired},
		{"empty baseURL", ag, "", pheroA2A.ErrBaseURLRequired},
		{"relative URL", ag, "not-a-url", pheroA2A.ErrInvalidBaseURL},
		{"valid", ag, "http://localhost:8080", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pheroA2A.New(tc.agent, tc.baseURL)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("New() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestAgentCard_Fields(t *testing.T) {
	ag := mustAgent(t, textLLM("x"), "my-agent", "does things")

	t.Run("name and description", func(t *testing.T) {
		srv, _ := pheroA2A.New(ag, "http://localhost:9000")
		card := srv.AgentCard()
		if card.Name != "my-agent" {
			t.Errorf("Name = %q, want %q", card.Name, "my-agent")
		}
		if card.Description != "does things" {
			t.Errorf("Description = %q, want %q", card.Description, "does things")
		}
	})

	t.Run("version override", func(t *testing.T) {
		srv, _ := pheroA2A.New(ag, "http://localhost:9000", pheroA2A.WithVersion("2.0"))
		if v := srv.AgentCard().Version; v != "2.0" {
			t.Errorf("Version = %q, want 2.0", v)
		}
	})

	t.Run("one interface without REST", func(t *testing.T) {
		srv, _ := pheroA2A.New(ag, "http://localhost:9000")
		if n := len(srv.AgentCard().SupportedInterfaces); n != 1 {
			t.Errorf("SupportedInterfaces len = %d, want 1", n)
		}
	})

	t.Run("two interfaces with REST", func(t *testing.T) {
		srv, _ := pheroA2A.New(ag, "http://localhost:9000", pheroA2A.WithRESTTransport())
		ifaces := srv.AgentCard().SupportedInterfaces
		if len(ifaces) != 2 {
			t.Fatalf("SupportedInterfaces len = %d, want 2", len(ifaces))
		}
		var hasREST bool
		for _, iface := range ifaces {
			if strings.HasSuffix(iface.URL, pheroA2A.RESTPathPrefix) {
				hasREST = true
			}
		}
		if !hasREST {
			t.Errorf("no interface URL ending with %q", pheroA2A.RESTPathPrefix)
		}
	})

	t.Run("streaming capability", func(t *testing.T) {
		srv, _ := pheroA2A.New(ag, "http://localhost:9000", pheroA2A.WithStreaming())
		if !srv.AgentCard().Capabilities.Streaming {
			t.Error("Capabilities.Streaming should be true after WithStreaming()")
		}
	})
}

// TestMount_Routes verifies that all expected URL paths are registered and
// routed correctly — including the /rest/ prefix-stripped path (regression
// guard for the http.StripPrefix fix).
func TestMount_Routes(t *testing.T) {
	ts, _ := newTestServer(t, textLLM("pong"), pheroA2A.WithRESTTransport())

	// Valid JSON-RPC envelope (method may be unknown, but routing must not 404).
	jsonRPCBody := `{"jsonrpc":"2.0","id":"1","method":"message/send","params":{"message":{"messageId":"m1","role":"user","parts":[{"kind":"text","text":"ping"}]}}}`

	tests := []struct {
		method  string
		path    string
		body    string
		notCode int // status code we must NOT see
	}{
		{"GET", "/.well-known/agent-card.json", "", http.StatusNotFound},
		{"POST", "/", jsonRPCBody, http.StatusNotFound},
		// Critical regression: /rest/message:send must be routed (not 404) after
		// the http.StripPrefix fix.
		{"POST", "/rest/message:send", jsonRPCBody, http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			var body *bytes.Buffer
			if tc.body != "" {
				body = bytes.NewBufferString(tc.body)
			} else {
				body = &bytes.Buffer{}
			}
			req, _ := http.NewRequest(tc.method, ts.URL+tc.path, body)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == tc.notCode {
				t.Errorf("%s %s returned %d, which must not happen", tc.method, tc.path, tc.notCode)
			}
		})
	}
}

// TestMount_RESTHandlerNil verifies that RESTHandler returns nil when
// WithRESTTransport is not used.
func TestMount_RESTHandlerNil(t *testing.T) {
	ag := mustAgent(t, textLLM("x"), "ag", "desc")
	srv, _ := pheroA2A.New(ag, "http://localhost:9000")
	if h := srv.RESTHandler(); h != nil {
		t.Error("RESTHandler() should be nil without WithRESTTransport()")
	}
}

// TestAgentCard_Skills verifies that the primary skill is always present and
// extra skills are appended.
func TestAgentCard_Skills(t *testing.T) {
	ag := mustAgent(t, textLLM("x"), "ag", "desc")
	extra := sdka2a.AgentSkill{ID: "s1", Name: "S1", Description: "extra"}
	srv, _ := pheroA2A.New(ag, "http://localhost:9000", pheroA2A.WithSkills(extra))
	skills := srv.AgentCard().Skills
	if len(skills) != 2 {
		t.Fatalf("want 2 skills, got %d", len(skills))
	}
	if skills[0].ID != "ag" {
		t.Errorf("primary skill ID = %q, want %q", skills[0].ID, "ag")
	}
	if skills[1].ID != "s1" {
		t.Errorf("extra skill ID = %q, want %q", skills[1].ID, "s1")
	}
}
