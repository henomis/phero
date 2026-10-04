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
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	natsclient "github.com/nats-io/nats.go"
)

const (
	chunkTypeResponse = "response"
	chunkTypeStatus   = "status"
	chunkTypeQuery    = "query"
	endpointPrompt    = "prompt"
	svcNameAgents     = "agents"
	attachmentsOkTrue = "true"
)

// envelope is the JSON request payload (§5.1).
type envelope struct {
	Prompt      string       `json:"prompt"`
	Attachments []attachment `json:"attachments,omitempty"`
}

// attachment is an inline file attachment (§5.2).
type attachment struct {
	Filename string `json:"filename"`
	// Content is standard-alphabet padded base64 (RFC 4648 §4).
	Content string `json:"content"`
}

// rawChunk is the on-wire form of a response stream chunk (§6.2).
type rawChunk struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// responseData is the object form of a response chunk's data field (§6.3).
type responseData struct {
	Text string `json:"text"`
}

// Service metadata keys (§3.2). The server writes them at registration and the
// client reads them back at discovery, so they are named once here rather than
// agreed between two files by literal.
const (
	metaAgent           = "agent"
	metaOwner           = "owner"
	metaSession         = "session"
	metaProtocolVersion = "protocol_version"
)

// heartbeatPayload is the JSON body published on the heartbeat subject (§8.3)
// and returned by the status endpoint (§8.7).
type heartbeatPayload struct {
	Agent      string `json:"agent"`
	Owner      string `json:"owner"`
	Session    string `json:"session,omitempty"`
	InstanceID string `json:"instance_id"`
	TS         string `json:"ts"`
	IntervalS  int    `json:"interval_s"`
	// ProtocolVersion and Endpoints are optional declarations (§8.3): they let
	// a heartbeat listener know where and how to prompt the instance. They are
	// not authoritative; the discovery record is (§3).
	ProtocolVersion string                       `json:"protocol_version,omitempty"`
	Endpoints       map[string]heartbeatEndpoint `json:"endpoints,omitempty"`
}

// heartbeatEndpoint declares one endpoint in a heartbeat (§8.3): its registered
// subject and its endpoint metadata, copied verbatim from the registration.
type heartbeatEndpoint struct {
	Subject  string            `json:"subject"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// serviceInfoResponse is a partial parse of the $SRV.INFO JSON response (§4).
type serviceInfoResponse struct {
	Name      string            `json:"name"`
	ID        string            `json:"id"`
	Metadata  map[string]string `json:"metadata"`
	Endpoints []endpointInfoRaw `json:"endpoints"`
}

type endpointInfoRaw struct {
	Name       string            `json:"name"`
	Subject    string            `json:"subject"`
	QueueGroup string            `json:"queue_group"`
	Metadata   map[string]string `json:"metadata"`
}

// NATS micro service error header names (§9.1).
const (
	errorCodeHeader = "Nats-Service-Error-Code"
	errorHeader     = "Nats-Service-Error"
)

// decodeEnvelope implements the §5.3 discrimination rule:
//
//  1. Skip leading UTF-8 whitespace.
//  2. If the next byte is '{', parse the remainder as JSON; reject if the
//     parsed object lacks a non-empty "prompt" field.
//  3. Otherwise, treat the entire original payload as plain text and promote
//     it to {"prompt": <payload>}.
//
// A zero-byte payload is rejected per §5.3.
func decodeEnvelope(data []byte) (*envelope, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: zero-byte payload", ErrMalformedEnvelope)
	}

	trimmed := bytes.TrimLeft(data, " \t\n\r")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: whitespace-only payload", ErrMalformedEnvelope)
	}

	if trimmed[0] == '{' {
		var env envelope
		if err := json.Unmarshal(trimmed, &env); err != nil {
			return nil, fmt.Errorf("%w: JSON parse error: %v", ErrMalformedEnvelope, err)
		}

		if env.Prompt == "" {
			return nil, fmt.Errorf("%w: missing or empty prompt field", ErrMalformedEnvelope)
		}

		return &env, nil
	}

	// Plain-text shorthand: original payload (not trimmed) becomes the prompt.
	return &envelope{Prompt: string(data)}, nil
}

// encodeResponseChunk encodes a response chunk using the bare-string data form (§6.3).
func encodeResponseChunk(text string) []byte {
	type chunk struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}

	b, _ := json.Marshal(chunk{Type: chunkTypeResponse, Data: text}) //nolint:errchkjson,lll // struct contains only string fields

	return b
}

// encodeResponseChunks splits text into response chunks (§6.3) whose encoded
// size is at most limit bytes, so that each one can be published on its own.
// Callers concatenate the chunks' text in order, so the split is invisible to
// them. Text that fits is sent as a single chunk, as before.
//
// The limit applies to the encoded chunk, not to the text: JSON escaping can
// make a piece of text several times larger (json.Marshal writes "<" as the six
// bytes <). Cuts fall on rune boundaries, so no chunk carries half a UTF-8
// character. It reports false when not even one rune fits under limit.
func encodeResponseChunks(text string, limit int) ([][]byte, bool) {
	if b := encodeResponseChunk(text); len(b) <= limit {
		return [][]byte{b}, true
	}

	overhead := len(encodeResponseChunk(""))

	var chunks [][]byte

	for text != "" {
		// Encoding never shrinks text, so no more than limit bytes of it can fit.
		n := min(len(text), limit)

		for {
			n = runeBoundary(text, n)
			if n == 0 {
				return nil, false
			}

			b := encodeResponseChunk(text[:n])
			if len(b) <= limit {
				chunks = append(chunks, b)
				text = text[n:]

				break
			}

			// Too big: shrink in proportion to how much the escaping grew this
			// piece, always making progress.
			next := 0
			if limit > overhead {
				next = n * (limit - overhead) / (len(b) - overhead)
			}

			n = min(next, n-1)
		}
	}

	return chunks, true
}

// runeBoundary moves n back to the start of the rune it falls in, so text[:n]
// does not end in the middle of a UTF-8 character. It backs off at most
// utf8.UTFMax-1 bytes: a longer run of continuation bytes is not valid UTF-8,
// and is cut where it stands.
func runeBoundary(text string, n int) int {
	if n <= 0 || n >= len(text) {
		return max(n, 0)
	}

	for back := 0; back < utf8.UTFMax-1 && n > 0 && !utf8.RuneStart(text[n]); back++ {
		n--
	}

	return n
}

// encodeStatusChunk encodes a status chunk (§6.4).
func encodeStatusChunk(status string) []byte {
	type chunk struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}

	b, _ := json.Marshal(chunk{Type: chunkTypeStatus, Data: status}) //nolint:errchkjson,lll // struct contains only string fields

	return b
}

// encodeHeartbeat serialises a heartbeat payload to JSON (§8.3).
func encodeHeartbeat(p heartbeatPayload) []byte {
	b, _ := json.Marshal(p) //nolint:errchkjson // only strings, ints and maps of them
	return b
}

// decodeHeartbeat parses a heartbeat or status reply (§8.3, §8.7).
//
// The optional declarations are decoded on their own: §8.3 says a receiver that
// finds protocol_version or endpoints malformed ignores that field and keeps the
// heartbeat. Decoding them as part of the payload would let one bad declaration
// drop the beat, and with it the instance's liveness.
func decodeHeartbeat(data []byte) (heartbeatPayload, error) {
	var raw struct {
		Agent           string          `json:"agent"`
		Owner           string          `json:"owner"`
		Session         string          `json:"session"`
		InstanceID      string          `json:"instance_id"`
		TS              string          `json:"ts"`
		IntervalS       int             `json:"interval_s"`
		ProtocolVersion json.RawMessage `json:"protocol_version"`
		Endpoints       json.RawMessage `json:"endpoints"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return heartbeatPayload{}, err
	}

	p := heartbeatPayload{
		Agent:      raw.Agent,
		Owner:      raw.Owner,
		Session:    raw.Session,
		InstanceID: raw.InstanceID,
		TS:         raw.TS,
		IntervalS:  raw.IntervalS,
	}

	if len(raw.ProtocolVersion) > 0 {
		_ = json.Unmarshal(raw.ProtocolVersion, &p.ProtocolVersion) // malformed: ignore the field
	}

	if len(raw.Endpoints) > 0 {
		var endpoints map[string]heartbeatEndpoint
		if json.Unmarshal(raw.Endpoints, &endpoints) == nil {
			p.Endpoints = endpoints
		}
	}

	return p, nil
}

// isTerminator returns true if msg is the zero-byte, headerless end-of-stream
// signal defined in §6.5.
func isTerminator(msg *natsclient.Msg) bool {
	return len(msg.Data) == 0 && len(msg.Header) == 0
}

// isServiceError returns true when the message carries NATS micro service
// error headers (§9.1).
func isServiceError(msg *natsclient.Msg) bool {
	return msg.Header.Get(errorCodeHeader) != ""
}

// parseServiceError extracts error information from a service-error message
// (§9.1) into a typed *ServiceError. A non-numeric or absent code yields Code 0,
// which [Permanent] treats as a transient fault.
//
// The JSON body is optional and read leniently: callers MUST tolerate an empty
// or non-JSON body and unknown fields (§9.1), so a field of the wrong type is
// skipped rather than costing the caller the code and description.
func parseServiceError(msg *natsclient.Msg) error {
	code, _ := strconv.Atoi(msg.Header.Get(errorCodeHeader))

	se := &ServiceError{
		Code:        code,
		Description: msg.Header.Get(errorHeader),
	}

	var body map[string]json.RawMessage
	if json.Unmarshal(msg.Data, &body) == nil {
		_ = json.Unmarshal(body["error"], &se.ErrCode)
		_ = json.Unmarshal(body["message"], &se.Message)

		var retryAfter float64
		if json.Unmarshal(body["retry_after_s"], &retryAfter) == nil &&
			retryAfter > 0 && retryAfter < maxRetryAfterSeconds {
			se.RetryAfter = time.Duration(retryAfter * float64(time.Second))
		}
	}

	if se.Message == "" {
		se.Message = se.Description
	}

	return se
}

// maxRetryAfterSeconds bounds retry_after_s to what a time.Duration can hold;
// a larger value is ignored rather than overflowing.
const maxRetryAfterSeconds = float64(math.MaxInt64 / int64(time.Second))

// queryData is the data field of a query chunk (§7.1).
type queryData struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
}

// parseQuery builds the error for a query chunk (§7.1). A malformed one is still
// a query — the agent is waiting all the same — so it yields a QueryError with
// whatever could be read.
func parseQuery(data json.RawMessage) error {
	var q queryData

	_ = json.Unmarshal(data, &q)

	return &QueryError{ID: q.ID, Prompt: q.Prompt}
}

// decodeResponseText extracts the text value from a response chunk data field.
// It handles both the bare-string form and the object form (§6.3).
func decodeResponseText(data json.RawMessage) string {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return text
	}

	var obj responseData
	if err := json.Unmarshal(data, &obj); err == nil {
		return obj.Text
	}

	return ""
}

// Size suffixes accepted in a max_payload string (§2.1).
const (
	unitB  = "B"
	unitKB = "KB"
	unitMB = "MB"
	unitGB = "GB"
)

// parseMaxPayload converts a size string ("512KB", "1MB", "4GB") to bytes.
func parseMaxPayload(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("nats: empty max_payload string")
	}

	type unit struct {
		suffix string
		mult   int64
	}

	units := []unit{
		{unitGB, 1 << 30},
		{unitMB, 1 << 20},
		{unitKB, 1 << 10},
		{unitB, 1},
	}

	for _, u := range units {
		if numStr, ok := strings.CutSuffix(s, u.suffix); ok {
			numStr = strings.TrimSpace(numStr)

			var n int64
			if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
				return 0, fmt.Errorf("nats: invalid max_payload %q: %w", s, err)
			}

			return n * u.mult, nil
		}
	}

	return 0, fmt.Errorf("nats: unrecognized unit in max_payload %q", s)
}

// encodeErrorBody builds the optional JSON body of a service error (§9.1),
// with retry_after_s only when retryAfter is positive.
func encodeErrorBody(errCode, message string, retryAfter time.Duration) []byte {
	type body struct {
		Error       string `json:"error"`
		Message     string `json:"message"`
		RetryAfterS int64  `json:"retry_after_s,omitempty"`
	}

	b, _ := json.Marshal(body{ //nolint:errchkjson // strings and an int
		Error:       errCode,
		Message:     message,
		RetryAfterS: retryAfterSeconds(retryAfter),
	})

	return b
}
