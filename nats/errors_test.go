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
	"errors"
	"fmt"
	"testing"
)

// TestServiceErrorIsErrServiceError verifies a *ServiceError still satisfies
// errors.Is(err, ErrServiceError) so existing sentinel checks keep working.
func TestServiceErrorIsErrServiceError(t *testing.T) {
	err := error(&ServiceError{Code: 400, Description: "bad request"})
	if !errors.Is(err, ErrServiceError) {
		t.Fatal("errors.Is(*ServiceError, ErrServiceError) = false, want true")
	}

	wrapped := fmt.Errorf("read agent: %w", err)
	if !errors.Is(wrapped, ErrServiceError) {
		t.Fatal("errors.Is(wrapped, ErrServiceError) = false, want true")
	}
}

// TestServiceErrorAs verifies the structured fields are recoverable through a
// wrapping chain via errors.As.
func TestServiceErrorAs(t *testing.T) {
	wrapped := fmt.Errorf("read agent: %w", &ServiceError{Code: 404, Description: "not found"})

	var se *ServiceError
	if !errors.As(wrapped, &se) {
		t.Fatal("errors.As(wrapped, &se) = false, want true")
	}

	if se.Code != 404 || se.Description != "not found" {
		t.Fatalf("got Code=%d Description=%q, want 404/\"not found\"", se.Code, se.Description)
	}
}

// TestServiceErrorClientError verifies the 4xx/non-4xx split.
func TestServiceErrorClientError(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{0, false}, // absent/non-numeric code
		{399, false},
		{400, true},
		{404, true},
		{499, true},
		{500, false},
		{503, false},
	}

	for _, tc := range cases {
		if got := (&ServiceError{Code: tc.code}).ClientError(); got != tc.want {
			t.Errorf("ServiceError{Code: %d}.ClientError() = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// TestServiceErrorErrorString verifies the rendered string matches the form the
// package has always emitted, so logs and any string consumers are unaffected.
func TestServiceErrorErrorString(t *testing.T) {
	got := (&ServiceError{Code: 400, Description: "bad request"}).Error()
	want := "nats: agent returned a service error: code=400 bad request"

	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
