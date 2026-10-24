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

package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/henomis/phero/v2/llm"
)

// failingServer answers every request with status, headers and an Anthropic
// error body.
func failingServer(t *testing.T, status int, headers map[string]string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"test_error","message":"nope"}}`))
	}))
	t.Cleanup(srv.Close)

	return srv
}

// TestProviderError verifies that an error the API answered with an HTTP status
// reaches callers as an llm.ProviderError, in buffered and streaming calls,
// with the wait the response asked for.
func TestProviderError(t *testing.T) {
	cases := []struct {
		status     int
		headers    map[string]string
		retryAfter time.Duration
	}{
		// Short, because the SDK honours it in its own retries before giving up.
		{http.StatusTooManyRequests, map[string]string{"Retry-After-Ms": "5"}, 5 * time.Millisecond},
		{http.StatusUnauthorized, nil, 0},
	}

	for _, tc := range cases {
		c := New("key", WithBaseURL(failingServer(t, tc.status, tc.headers).URL))
		msgs := []llm.Message{llm.UserMessage(llm.Text("hi"))}

		_, execErr := c.Execute(context.Background(), msgs)

		var streamErr error

		for _, err := range c.ExecuteStream(context.Background(), msgs) {
			if err != nil {
				streamErr = err
				break
			}
		}

		for name, err := range map[string]error{"Execute": execErr, "ExecuteStream": streamErr} {
			pe, ok := errors.AsType[*llm.ProviderError](err)
			if !ok {
				t.Fatalf("%s status %d: error %v is not an *llm.ProviderError", name, tc.status, err)
			}

			if pe.Provider != "anthropic" || pe.StatusCode != tc.status || pe.RetryAfter != tc.retryAfter {
				t.Fatalf("%s status %d: ProviderError = %+v, want RetryAfter %v", name, tc.status, pe, tc.retryAfter)
			}
		}
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		headers map[string]string
		want    time.Duration
	}{
		{"none", nil, 0},
		{"milliseconds win", map[string]string{"Retry-After-Ms": "1500", "Retry-After": "30"}, 1500 * time.Millisecond},
		{"seconds", map[string]string{"Retry-After": "30"}, 30 * time.Second},
		{"fractional seconds", map[string]string{"Retry-After": "0.5"}, 500 * time.Millisecond},
		{"http date", map[string]string{"Retry-After": now.Add(time.Minute).Format(http.TimeFormat)}, time.Minute},
		{"date in the past", map[string]string{"Retry-After": now.Add(-time.Minute).Format(http.TimeFormat)}, 0},
		{"negative", map[string]string{"Retry-After": "-5"}, 0},
		{"garbage", map[string]string{"Retry-After": "soon"}, 0},
		{"bad milliseconds fall back", map[string]string{"Retry-After-Ms": "x", "Retry-After": "2"}, 2 * time.Second},
	}

	for _, tc := range cases {
		h := http.Header{}
		for k, v := range tc.headers {
			h.Set(k, v)
		}

		if got := retryAfter(h, now); got != tc.want {
			t.Errorf("%s: retryAfter = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsContextLengthError(t *testing.T) {
	cases := []struct {
		status  int
		message string
		want    bool
	}{
		{http.StatusBadRequest, "prompt is too long: 210000 tokens > 200000 maximum", true},
		{http.StatusRequestEntityTooLarge, "request_too_large", true},
		{http.StatusBadRequest, "messages: field required", false},
		{http.StatusTooManyRequests, "prompt is too long", false},
	}

	for _, tc := range cases {
		if got := isContextLengthError(tc.status, tc.message); got != tc.want {
			t.Errorf("isContextLengthError(%d, %q) = %v, want %v", tc.status, tc.message, got, tc.want)
		}
	}
}

func TestToolCallInput(t *testing.T) {
	cases := map[string]int{
		`{"path": "a.go"}`: 1,
		`{"path": "a.go"`:  0, // cut off at the token limit
		`[1, 2]`:           0,
		`null`:             0,
		``:                 0,
	}

	for args, want := range cases {
		if got := toolCallInput(args); got == nil || len(got) != want {
			t.Errorf("toolCallInput(%q) = %v, want an object with %d keys", args, got, want)
		}
	}
}
