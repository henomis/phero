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

package llm_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"

	"github.com/henomis/phero/v2/llm"
)

func TestIsRetryable(t *testing.T) {
	provider := func(status int) error {
		return &llm.ProviderError{Provider: "p", StatusCode: status, Err: errors.New("api")}
	}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", provider(429), true},
		{"server error", provider(500), true},
		{"overloaded", provider(529), true},
		{"request timeout", provider(408), true},
		{"bad request", provider(400), false},
		{"unauthorized", provider(401), false},
		{"context length", &llm.ProviderError{StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("long")}, false},
		{"wrapped context length", fmt.Errorf("call: %w", llm.ErrContextLengthExceeded), false},
		{"guardrail", fmt.Errorf("blocked: %w", llm.ErrGuardrailBlocked), false},
		{"canceled", context.Canceled, false},
		{"deadline", fmt.Errorf("post: %w", context.DeadlineExceeded), true},
		{"network", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"unknown", errors.New("something else"), false},
	}

	for _, tc := range cases {
		if got := llm.IsRetryable(tc.err); got != tc.want {
			t.Errorf("%s: IsRetryable = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestProviderError_IsContextLengthExceeded(t *testing.T) {
	long := fmt.Errorf("execute: %w", &llm.ProviderError{StatusCode: 400, ContextLengthExceeded: true, Err: errors.New("x")})
	if !errors.Is(long, llm.ErrContextLengthExceeded) {
		t.Fatal("ContextLengthExceeded error does not match ErrContextLengthExceeded")
	}

	other := &llm.ProviderError{StatusCode: 400, Err: errors.New("x")}
	if errors.Is(other, llm.ErrContextLengthExceeded) {
		t.Fatal("plain 400 matches ErrContextLengthExceeded")
	}
}
