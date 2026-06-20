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
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	pheroA2A "github.com/henomis/phero/a2a"
)

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"my-agent", "my-agent"},
		{"My Agent", "My_Agent"},
		{"agent v2.0!", "agent_v2_0_"},
		{strings.Repeat("a", 70), strings.Repeat("a", 64)},
		{"", "agent"},
		{"!@#$", "____"},
	}
	for _, tc := range tests {
		got := pheroA2A.SanitizeToolName(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeToolName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewClient_Validation(t *testing.T) {
	ctx := context.Background()
	// These validate before any network call, so no server needed.
	if _, err := pheroA2A.NewClient(ctx, ""); !errors.Is(err, pheroA2A.ErrURLRequired) {
		t.Errorf("empty URL: got %v, want ErrURLRequired", err)
	}

	if _, err := pheroA2A.NewClient(ctx, "not-a-url"); !errors.Is(err, pheroA2A.ErrInvalidBaseURL) {
		t.Errorf("invalid URL: got %v, want ErrInvalidBaseURL", err)
	}
}

// TestAsTool_Success spins up a real httptest server with a stub LLM, wraps it
// as a tool via the client, calls it, and verifies the response.
func TestAsTool_Success(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts := newTestServer(t, textLLM("pong"))

	client, err := pheroA2A.NewClient(ctx, ts.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tool, err := client.AsTool()
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}

	result, err := tool.Handle(ctx, `{"input":"ping"}`)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if result == nil {
		t.Fatal("Handle returned nil result")
	}

	// Serialize to JSON and check the output field — toolOutput is unexported.
	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), "pong") {
		t.Errorf("serialized result = %s, want to contain %q", b, "pong")
	}
}

// TestAsTool_RESTTransport verifies that the strip-prefix fix works end-to-end:
// a client preferring HTTP+JSON/SSE can reach the agent via /rest/.
func TestAsTool_RESTTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ts := newTestServer(t, textLLM("rest-pong"), pheroA2A.WithRESTTransport())

	client, err := pheroA2A.NewClient(ctx, ts.URL,
		pheroA2A.WithPreferredTransports(sdka2a.TransportProtocolHTTPJSON),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tool, err := client.AsTool()
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}

	result, err := tool.Handle(ctx, `{"input":"ping"}`)
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}

	b, _ := json.Marshal(result)
	if !strings.Contains(string(b), "rest-pong") {
		t.Errorf("serialized result = %s, want to contain %q", b, "rest-pong")
	}
}

// TestAsTool_TaskFailed_WithReason verifies that when the agent fails, the
// returned error wraps ErrTaskFailed and includes the failure reason.
func TestAsTool_TaskFailed_WithReason(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sentinel := errors.New("model quota exceeded")
	ts := newTestServer(t, errLLM(sentinel))

	client, err := pheroA2A.NewClient(ctx, ts.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	tool, err := client.AsTool()
	if err != nil {
		t.Fatalf("AsTool: %v", err)
	}

	_, err = tool.Handle(ctx, `{"input":"trigger failure"}`)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if !errors.Is(err, pheroA2A.ErrTaskFailed) {
		t.Errorf("errors.Is(ErrTaskFailed) = false; err = %v", err)
	}

	if !strings.Contains(err.Error(), "model quota exceeded") {
		t.Errorf("error message = %q, want to contain the failure reason", err.Error())
	}
}
