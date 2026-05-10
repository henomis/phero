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

import "errors"

var (
	// ErrNilConn is returned when a nil *nats.Conn is passed to New or NewClient.
	ErrNilConn = errors.New("nats: NATS connection must not be nil")

	// ErrNilAgent is returned when a nil *agent.Agent is passed to New.
	ErrNilAgent = errors.New("nats: agent must not be nil")

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
