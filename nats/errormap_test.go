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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

// errHandler fails every run with err.
type errHandler struct{ err error }

func (h errHandler) Run(context.Context, ...llm.ContentPart) (*agent.Result, error) {
	return nil, h.err
}

// sentError is what a caller would read from a failed stream.
type sentError struct {
	code, description string
	ErrCode           string `json:"error"`
	Message           string `json:"message"`
	RetryAfterS       *int64 `json:"retry_after_s"`
}

// runFailing runs a prompt against a handler failing with err and returns the
// error the server sent, checking that the stream ends with the terminator.
func runFailing(t *testing.T, err error, opts ...ServerOption) sentError {
	t.Helper()

	return runFailingIn(context.Background(), t, err, opts...)
}

// runFailingIn is runFailing with the context the run is given.
func runFailingIn(ctx context.Context, t *testing.T, err error, opts ...ServerOption) sentError {
	t.Helper()

	s, _ := newTestServer(t, time.Second)

	for _, opt := range opts {
		opt(s.cfg)
	}

	s.handler = errHandler{err: err}

	req := &fakeRequest{data: []byte("hi")}
	s.processPrompt(ctx, req)

	if req.errCode == "" {
		t.Fatal("no service error was sent")
	}

	if last := req.responses[len(req.responses)-1]; len(last) != 0 {
		t.Fatal("the error is not followed by the terminator")
	}

	got := sentError{code: req.errCode, description: req.errDesc}
	if jsonErr := json.Unmarshal(req.errBody, &got); jsonErr != nil {
		t.Fatalf("error body is not JSON: %v (%s)", jsonErr, req.errBody)
	}

	return got
}

func TestErrorFor_CodedError(t *testing.T) {
	ce := &CodedError{Code: 429, ErrCode: "quota", Message: "slow down", RetryAfter: 1500 * time.Millisecond}

	got := runFailing(t, fmt.Errorf("run: %w", ce))

	if got.code != "429" || got.ErrCode != "quota" || got.Message != "slow down" || got.description != "slow down" {
		t.Fatalf("sent %+v", got)
	}

	// Rounded up, so a caller never comes back early.
	if got.RetryAfterS == nil || *got.RetryAfterS != 2 {
		t.Fatalf("retry_after_s = %v, want 2", got.RetryAfterS)
	}
}

// TestErrorFor_CodedErrorIsNormalised verifies the protocol's limits: a code
// outside the §9.2 table is sent as 500, empty fields are filled in, and the
// header never carries a line break.
func TestErrorFor_CodedErrorIsNormalised(t *testing.T) {
	got := runFailing(t, &CodedError{Code: 503, ErrCode: "maintenance", Message: "down\nfor maintenance"})
	if got.code != "500" || got.ErrCode != "maintenance" {
		t.Fatalf("sent %+v, want 500 keeping its ErrCode", got)
	}

	if got.description != "down for maintenance" || got.Message != "down\nfor maintenance" {
		t.Fatalf("description = %q, message = %q", got.description, got.Message)
	}

	if got.RetryAfterS != nil {
		t.Fatalf("retry_after_s = %d, want it omitted", *got.RetryAfterS)
	}

	got = runFailing(t, &CodedError{Code: 404})
	if got.code != "404" || got.ErrCode != errCodeNotFound || got.description == "" {
		t.Fatalf("sent %+v, want 404 not_found with a description", got)
	}
}

func TestErrorFor_PlainErrorIs500(t *testing.T) {
	got := runFailing(t, errors.New("boom"))
	if got.code != "500" || got.ErrCode != errCodeInternal || got.Message != "boom" {
		t.Fatalf("sent %+v", got)
	}

	// micro sends nothing for an empty description; the caller would read an
	// empty answer.
	got = runFailing(t, errors.New(""))
	if got.code != "500" || got.description == "" {
		t.Fatalf("sent %+v, want a 500 with a description", got)
	}
}

func TestErrorFor_MapperComesFirst(t *testing.T) {
	mapper := WithErrorMapper(func(err error) *CodedError {
		if strings.Contains(err.Error(), "secret") {
			return &CodedError{Code: 500, ErrCode: "redacted", Message: "internal error"}
		}

		return nil
	})

	got := runFailing(t, &CodedError{Code: 400, Message: "secret detail"}, mapper)
	if got.ErrCode != "redacted" || strings.Contains(got.Message+got.description, "secret") {
		t.Fatalf("sent %+v, want the mapper's redacted error", got)
	}

	got = runFailing(t, &CodedError{Code: 400, Message: "bad prompt"}, mapper)
	if got.code != "400" {
		t.Fatalf("sent %+v, want the CodedError once the mapper returns nil", got)
	}
}

// TestErrorFor_ProviderErrors covers the default mapping: only a rate limit
// keeps its code; the rest are the agent's problem, not the caller's.
func TestErrorFor_ProviderErrors(t *testing.T) {
	cases := []struct {
		status  int
		code    string
		errCode string
	}{
		{429, "429", errCodeRateLimited},
		{401, "500", errCodeLLMAuthFailed},
		{403, "500", errCodeLLMAuthFailed},
		{400, "500", errCodeLLMRejected},
		{413, "500", errCodeLLMRejected},
		{422, "500", errCodeLLMRejected},
		{402, "500", errCodeLLMError},
		{503, "500", errCodeLLMError},
	}

	for _, tc := range cases {
		pe := &llm.ProviderError{
			Provider: "test", StatusCode: tc.status, RetryAfter: 30 * time.Second,
			Err: fmt.Errorf("provider said %d", tc.status),
		}

		got := runFailing(t, fmt.Errorf("agent: %w", pe))
		if got.code != tc.code || got.ErrCode != tc.errCode {
			t.Errorf("provider %d: sent %s/%s, want %s/%s", tc.status, got.code, got.ErrCode, tc.code, tc.errCode)
		}

		if tc.status == 429 && (got.RetryAfterS == nil || *got.RetryAfterS != 30) {
			t.Errorf("provider 429: retry_after_s = %v, want 30", got.RetryAfterS)
		}
	}
}

// TestErrorFor_DrainCancellation verifies that a run cut short by a drain is
// reported as the drain it is, like a prompt refused during one.
func TestErrorFor_DrainCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := runFailingIn(ctx, t, fmt.Errorf("llm call: %w", context.Canceled))
	if got.code != "500" || got.ErrCode != errCodeServerDraining {
		t.Fatalf("sent %+v, want 500 %s", got, errCodeServerDraining)
	}

	// A handler's own cancellation, with the server still running, is not a drain.
	got = runFailing(t, context.Canceled)
	if got.ErrCode != errCodeInternal {
		t.Fatalf("sent %+v, want %s", got, errCodeInternal)
	}
}

// TestErrorFor_AgentLLMRateLimit follows a provider's 429 through a real agent:
// the agent returns the LLM's error with its chain intact, so the caller sees a
// retryable 429 with the provider's wait.
func TestErrorFor_AgentLLMRateLimit(t *testing.T) {
	limited := llm.Func(func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
		return nil, &llm.ProviderError{
			Provider: "test", StatusCode: 429, RetryAfter: 20 * time.Second, Err: errors.New("rate limited"),
		}
	})

	a, err := agent.New(limited, "worker", "answers questions")
	if err != nil {
		t.Fatalf("agent.New: %v", err)
	}

	s, ctx := newTestServer(t, time.Second)
	s.handler = a

	req := &fakeRequest{data: []byte("hi")}
	s.processPrompt(ctx, req)

	var body sentError
	if jsonErr := json.Unmarshal(req.errBody, &body); jsonErr != nil {
		t.Fatalf("error body: %v", jsonErr)
	}

	if req.errCode != "429" || body.RetryAfterS == nil || *body.RetryAfterS != 20 {
		t.Fatalf("sent %s %s, want 429 with retry_after_s 20", req.errCode, req.errBody)
	}
}
