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
)

var (
	// ErrNilConn is returned when a nil *nats.Conn is passed to New or NewClient.
	ErrNilConn = errors.New("nats: NATS connection must not be nil")

	// ErrNilHandler is returned when a nil Handler is passed to New.
	ErrNilHandler = errors.New("nats: handler must not be nil")

	// ErrEmptyOwner is returned when the owner field is empty.
	ErrEmptyOwner = errors.New("nats: owner must not be empty")

	// ErrEmptyName is returned when the instance name field is empty.
	ErrEmptyName = errors.New("nats: instance name must not be empty")

	// ErrEmptyPrompt is returned when the prompt text is empty (§5.1).
	ErrEmptyPrompt = errors.New("nats: prompt must not be empty")

	// ErrPayloadTooLarge is returned when the encoded request exceeds the
	// agent's advertised max_payload (§5.4).
	ErrPayloadTooLarge = errors.New("nats: payload exceeds agent max_payload")

	// ErrAttachmentsNotAllowed is returned when attachments are sent to an
	// agent whose attachments_ok metadata is false (§5.4).
	ErrAttachmentsNotAllowed = errors.New("nats: agent does not accept attachments")

	// ErrNoAgentsFound is returned by Discover when no compliant agents
	// respond within the discovery timeout.
	ErrNoAgentsFound = errors.New("nats: no compliant agents discovered")

	// ErrStreamTimeout is returned when the stream inactivity timeout fires
	// without receiving the next chunk or terminator (§6.6).
	ErrStreamTimeout = errors.New("nats: stream inactivity timeout")

	// ErrServiceError is returned when the agent responds with NATS micro
	// service error headers (§9).
	ErrServiceError = errors.New("nats: agent returned a service error")

	// ErrMalformedEnvelope is returned when the request payload cannot be
	// decoded as a valid envelope (§5.3).
	ErrMalformedEnvelope = errors.New("nats: malformed request envelope")
)

// ServiceError carries the structured detail of a NATS micro service error
// response (§9.1): the numeric status Code and its Description. It wraps
// ErrServiceError, so errors.Is(err, ErrServiceError) keeps matching; use
// errors.As(err, &se) to read the fields. Callers can classify the fault
// without parsing the error string — a 4xx Code (see ClientError) means the
// agent rejected the request as malformed, so a retry cannot help, whereas a
// 5xx Code is a transient server-side failure.
type ServiceError struct {
	// Code is the micro service error code (Nats-Service-Error-Code header),
	// 0 when the agent sent a non-numeric or absent code.
	Code int
	// Description is the human-readable error text (Nats-Service-Error header).
	Description string
}

// Error renders the same form the package has always produced
// ("nats: agent returned a service error: code=<n> <desc>") so existing log
// output and string matches are unaffected.
func (e *ServiceError) Error() string {
	return fmt.Sprintf("%s: code=%d %s", ErrServiceError.Error(), e.Code, e.Description)
}

// Unwrap reports ErrServiceError so errors.Is(err, ErrServiceError) matches a
// *ServiceError.
func (e *ServiceError) Unwrap() error { return ErrServiceError }

// ClientError reports whether the code is 4xx-class — the agent rejected the
// request as malformed (a permanent fault), as opposed to a 5xx transient
// server-side failure.
func (e *ServiceError) ClientError() bool {
	return e.Code >= 400 && e.Code < 500
}
