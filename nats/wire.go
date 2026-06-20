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
	"strings"

	natsclient "github.com/nats-io/nats.go"
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

// heartbeatPayload is the JSON body published on the heartbeat subject (§8.3)
// and returned by the status endpoint (§8.7).
type heartbeatPayload struct {
	Agent      string `json:"agent"`
	Owner      string `json:"owner"`
	Session    string `json:"session,omitempty"`
	InstanceID string `json:"instance_id"`
	Ts         string `json:"ts"`
	IntervalS  int    `json:"interval_s"`
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

	b, _ := json.Marshal(chunk{Type: "response", Data: text})

	return b
}

// encodeStatusChunk encodes a status chunk (§6.4).
func encodeStatusChunk(status string) []byte {
	type chunk struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}

	b, _ := json.Marshal(chunk{Type: "status", Data: status})

	return b
}

// encodeHeartbeat serialises a heartbeat payload to JSON (§8.3).
func encodeHeartbeat(p heartbeatPayload) []byte {
	b, _ := json.Marshal(p)
	return b
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

// parseServiceError extracts error information from a service-error message (§9.1).
func parseServiceError(msg *natsclient.Msg) error {
	code := msg.Header.Get(errorCodeHeader)
	desc := msg.Header.Get(errorHeader)

	return fmt.Errorf("%w: code=%s %s", ErrServiceError, code, desc)
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
		{"GB", 1 << 30},
		{"MB", 1 << 20},
		{"KB", 1 << 10},
		{"B", 1},
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

// encodeErrorBody builds a §9.1 JSON error body.
func encodeErrorBody(errCode, message string) []byte {
	type body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}

	b, _ := json.Marshal(body{Error: errCode, Message: message})

	return b
}
