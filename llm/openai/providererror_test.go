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

package openai_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/llm/openai"
)

// failingServer answers every request with status and an OpenAI error body.
func failingServer(t *testing.T, status int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"nope","type":"test_error","code":"test"}}`))
	}))
	t.Cleanup(srv.Close)

	return srv
}

// TestProviderError verifies that an error the API answered with an HTTP status
// reaches callers as an llm.ProviderError, in buffered and streaming calls, with
// the SDK's error still reachable underneath.
func TestProviderError(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusBadGateway} {
		c := openai.New("key", openai.WithBaseURL(failingServer(t, status).URL+"/v1"))
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
				t.Fatalf("%s status %d: error %v is not an *llm.ProviderError", name, status, err)
			}

			if pe.Provider != "openai" || pe.StatusCode != status || pe.Err == nil {
				t.Fatalf("%s status %d: ProviderError = %+v", name, status, pe)
			}

			if pe.Error() != pe.Err.Error() {
				t.Fatalf("%s: Error() = %q, want the SDK's text unchanged", name, pe.Error())
			}
		}
	}
}
