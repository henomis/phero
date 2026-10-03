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
	"errors"
	"testing"
	"time"

	natsclient "github.com/nats-io/nats.go"
)

// scriptedStream is a Stream that replays msgs, then reports a timeout.
func scriptedStream(msgs ...*natsclient.Msg) *Stream {
	return &Stream{
		next: func(context.Context) (*natsclient.Msg, error) {
			if len(msgs) == 0 {
				return nil, natsclient.ErrTimeout
			}

			msg := msgs[0]
			msgs = msgs[1:]

			return msg, nil
		},
	}
}

func dataMsg(data string) *natsclient.Msg { return &natsclient.Msg{Data: []byte(data)} }

func terminatorMsg() *natsclient.Msg { return &natsclient.Msg{} }

func errorMsg(code, description, body string) *natsclient.Msg {
	h := natsclient.Header{}
	h.Set(errorCodeHeader, code)
	h.Set(errorHeader, description)

	return &natsclient.Msg{Header: h, Data: []byte(body)}
}

// TestText_QueryEndsTheStream is issue #4: a query chunk must end the call with
// the question, instead of being skipped while the agent waits for an answer.
func TestText_QueryEndsTheStream(t *testing.T) {
	s := scriptedStream(
		dataMsg(`{"type":"status","data":"ack"}`),
		dataMsg(`{"type":"response","data":"Planning... "}`),
		dataMsg(`{"type":"query","data":{"id":"q-1","reply_subject":"_INBOX.x","prompt":"Delete 200 files? (yes/no)"}}`),
		dataMsg(`{"type":"response","data":"never read"}`),
		terminatorMsg(),
	)

	text, err := s.Text(context.Background())

	var qe *QueryError
	if !errors.As(err, &qe) {
		t.Fatalf("Text error = %v, want a *QueryError", err)
	}

	if qe.ID != "q-1" || qe.Prompt != "Delete 200 files? (yes/no)" {
		t.Fatalf("QueryError = %+v", qe)
	}

	if !errors.Is(err, ErrQueryNotSupported) || !Permanent(err) {
		t.Fatalf("a query must match ErrQueryNotSupported and be permanent: %v", err)
	}

	if text != "" {
		t.Fatalf("text = %q, want the partial answer discarded", text)
	}
}

// TestText_MalformedQueryIsStillAQuery verifies that a query chunk whose data
// cannot be read still ends the call: the agent is waiting either way.
func TestText_MalformedQueryIsStillAQuery(t *testing.T) {
	s := scriptedStream(
		dataMsg(`{"type":"status","data":"ack"}`),
		dataMsg(`{"type":"query","data":"not an object"}`),
	)

	if _, err := s.Text(context.Background()); !errors.Is(err, ErrQueryNotSupported) {
		t.Fatalf("Text error = %v, want ErrQueryNotSupported", err)
	}
}

func TestText_RateLimitIsRetryable(t *testing.T) {
	s := scriptedStream(
		errorMsg("429", "Too Many Requests", `{"error":"rate_limited","message":"slow down","retry_after_s":30}`),
		terminatorMsg(),
	)

	_, err := s.Text(context.Background())

	var se *ServiceError
	if !errors.As(err, &se) {
		t.Fatalf("Text error = %v, want a *ServiceError", err)
	}

	if se.Code != 429 || se.ErrCode != errCodeRateLimited || se.Message != "slow down" || se.RetryAfter != 30*time.Second {
		t.Fatalf("ServiceError = %+v", se)
	}

	if Permanent(err) {
		t.Fatal("a 429 must be retryable")
	}
}

// TestPermanent_ServiceErrorCodes is issue #3: every 4xx is permanent except a
// 429, which a later attempt may get past.
func TestPermanent_ServiceErrorCodes(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{400, true},
		{401, true},
		{403, true},
		{404, true},
		{409, true},
		{429, false},
		{500, false},
		{0, false}, // absent or non-numeric code
	}

	for _, tc := range cases {
		if got := Permanent(&ServiceError{Code: tc.code}); got != tc.want {
			t.Errorf("Permanent(code %d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// TestParseServiceError_Body covers the optional JSON body (§9.1): its fields
// are read when present and well-typed, and never cost the code or description.
func TestParseServiceError_Body(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		errCode    string
		message    string
		retryAfter time.Duration
	}{
		{
			name: "full body", body: `{"error":"rate_limited","message":"busy","retry_after_s":30}`,
			errCode: errCodeRateLimited, message: "busy", retryAfter: 30 * time.Second,
		},
		{
			name: "no message falls back to the header", body: `{"error":"rate_limited"}`,
			errCode: errCodeRateLimited, message: "header text",
		},
		{
			name: "fractional retry", body: `{"error":"rate_limited","retry_after_s":1.5}`,
			errCode: errCodeRateLimited, message: "header text", retryAfter: 1500 * time.Millisecond,
		},
		{
			name: "negative retry is ignored", body: `{"error":"x","retry_after_s":-5}`,
			errCode: "x", message: "header text",
		},
		{
			name: "string retry is ignored", body: `{"error":"x","retry_after_s":"30"}`,
			errCode: "x", message: "header text",
		},
		{
			name: "huge retry is ignored", body: `{"error":"x","retry_after_s":1e300}`,
			errCode: "x", message: "header text",
		},
		{
			name: "wrong-typed error field is skipped", body: `{"error":42,"message":"kept"}`,
			message: "kept",
		},
		{name: "empty body", body: ``, message: "header text"},
		{name: "plain-text body", body: `something broke`, message: "header text"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var se *ServiceError
			if !errors.As(parseServiceError(errorMsg("429", "header text", tc.body)), &se) {
				t.Fatal("parseServiceError did not return a *ServiceError")
			}

			if se.Code != 429 || se.Description != "header text" {
				t.Fatalf("code/description = %d/%q, want 429/%q", se.Code, se.Description, "header text")
			}

			if se.ErrCode != tc.errCode || se.Message != tc.message || se.RetryAfter != tc.retryAfter {
				t.Fatalf("got ErrCode=%q Message=%q RetryAfter=%v, want %q %q %v",
					se.ErrCode, se.Message, se.RetryAfter, tc.errCode, tc.message, tc.retryAfter)
			}
		})
	}
}
