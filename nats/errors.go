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
	"time"
)

var (
	// ErrNilConn is returned when a nil *nats.Conn is passed to New or NewClient.
	ErrNilConn = errors.New("nats: NATS connection must not be nil")

	// ErrNilHandler is returned when a nil Handler is passed to New.
	ErrNilHandler = errors.New("nats: handler must not be nil")

	// ErrNilClient is returned when a nil *Client is passed to NewResolver.
	ErrNilClient = errors.New("nats: client must not be nil")

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

	// ErrQueryNotSupported is returned by [Stream.Text] when the agent asks the
	// caller a question mid-stream (§7). This client does not answer queries;
	// the returned error is a *[QueryError] carrying the question.
	ErrQueryNotSupported = errors.New("nats: agent asked a question this client cannot answer")

	// ErrMalformedEnvelope is returned when the request payload cannot be
	// decoded as a valid envelope (§5.3).
	ErrMalformedEnvelope = errors.New("nats: malformed request envelope")

	// ErrDrainIncomplete is returned by [Server.Drain], and by [Server.Start] on
	// shutdown, when prompt handlers were still running after the drain budget
	// was spent *and* cancelling them did not make them return. The server has
	// stopped accepting and its subscriptions are gone; those goroutines are not.
	ErrDrainIncomplete = errors.New("nats: drain did not complete")

	// ErrServerStopped is returned by [Server.Start] on a Server that has
	// already drained. A Server serves once; build a new one to serve again.
	ErrServerStopped = errors.New("nats: server has been stopped")

	// ErrInvalidMaxPayload is returned by [New] when the advertised max_payload
	// cannot be parsed, or when a configured one exceeds what the connection
	// will carry.
	ErrInvalidMaxPayload = errors.New("nats: invalid max_payload")

	// ErrInvalidSubjectToken is returned by [ValidateSubjectToken], and by [New]
	// for an owner, name, session or agent id that cannot safely become a token
	// of the protocol's subject hierarchy (§3.2).
	ErrInvalidSubjectToken = errors.New("nats: invalid subject token")
)

// Permanent reports whether err is a fault that retrying cannot fix.
//
// The taxonomy belongs here, beside the errors it classifies, because a caller
// that enumerates them is holding a copy of phero's knowledge: when phero adds
// an error kind, that copy silently misfiles it. Misfiling in the direction
// callers default to — retryable — means a new permanent fault is retried to
// exhaustion at LLM prices before it is reported, and reported as "gave up"
// rather than as the fault that caused it.
//
// Permanent covers a request the agent will reject however often it arrives (an
// empty or oversized prompt, a rejected attachment, a malformed envelope, a
// 4xx-class service error other than 429, a question the client cannot answer)
// and the construction faults that mean the caller is misconfigured (a nil
// dependency, an empty or invalid identifier).
//
// Everything else is treated as worth another attempt, including
// [ErrNoAgentsFound], [ErrStreamTimeout] and a 429 rate limit: nobody answering
// *now*, a stream going quiet and an agent that is busy are all states a later
// attempt may not meet. A 429 may say how long to wait in
// [ServiceError.RetryAfter]. Context
// errors are deliberately not permanent — the deadline belongs to the caller,
// and a fresh one may well succeed.
func Permanent(err error) bool {
	if err == nil {
		return false
	}

	switch {
	case errors.Is(err, ErrEmptyPrompt),
		errors.Is(err, ErrPayloadTooLarge),
		errors.Is(err, ErrAttachmentsNotAllowed),
		errors.Is(err, ErrMalformedEnvelope),
		errors.Is(err, ErrQueryNotSupported),
		errors.Is(err, ErrInvalidSubjectToken),
		errors.Is(err, ErrInvalidMaxPayload),
		errors.Is(err, ErrNilConn),
		errors.Is(err, ErrNilHandler),
		errors.Is(err, ErrNilClient),
		errors.Is(err, ErrEmptyOwner),
		errors.Is(err, ErrEmptyName),
		errors.Is(err, ErrServerStopped):
		return true
	}

	// A 4xx service error means the agent rejected the request itself, except
	// a 429: the agent is rate limited, and the same request may well succeed
	// later. Read from the typed error rather than the string.
	var svcErr *ServiceError
	if errors.As(err, &svcErr) {
		return svcErr.ClientError() && svcErr.Code != codeTooManyRequests
	}

	return false
}

// CodedError lets a [Handler] choose the error a caller receives (§9.1): a
// status code from the §9.2 table, a machine-readable ErrCode, a Message, and
// how long the caller should wait before retrying. Return it from Run, directly
// or wrapped; the server finds it with errors.As.
//
// Code must be one of 400, 401, 403, 404, 409, 429 or 500 — the protocol
// allows no others — and any other value is sent as 500. An empty ErrCode is
// filled in from the code ("rate_limited" for 429), and an empty Message from
// Err.
//
// A tool's error does not fail an agent's run (the agent sees it and carries
// on), so a CodedError returned by a tool never reaches the caller. To shape
// errors you did not create, such as those from the LLM, use [WithErrorMapper].
//
// CodedError is the server's side of the exchange; the caller receives a
// *[ServiceError]. They are separate types so that a handler relaying another
// agent's ServiceError does not pass that agent's verdict off as its own.
type CodedError struct {
	// Code is the §9.2 status code.
	Code int
	// ErrCode is the body's stable machine-readable code ("error").
	ErrCode string
	// Message is the human-readable detail, sent as the body's "message" and
	// the Nats-Service-Error header.
	Message string
	// RetryAfter, when positive, is sent as "retry_after_s": how long the
	// caller should wait before trying again, typically with a 429.
	RetryAfter time.Duration
	// Err is the underlying cause, for errors.Is/As and the server's own logs.
	Err error
}

func (e *CodedError) Error() string {
	switch {
	case e.Message != "":
		return fmt.Sprintf("nats: code=%d %s", e.Code, e.Message)
	case e.Err != nil:
		return fmt.Sprintf("nats: code=%d %v", e.Code, e.Err)
	default:
		return fmt.Sprintf("nats: code=%d", e.Code)
	}
}

// Unwrap returns the underlying cause.
func (e *CodedError) Unwrap() error { return e.Err }

// codeTooManyRequests is the §9.2 rate-limit code, the one 4xx a retry can fix.
const codeTooManyRequests = 429

// ServiceError carries the structured detail of a NATS micro service error
// response (§9.1): the numeric status Code and its Description from the
// headers, and the optional JSON body. It wraps ErrServiceError, so
// errors.Is(err, ErrServiceError) keeps matching; use errors.As(err, &se) to
// read the fields. Callers can classify the fault without parsing the error
// string, or let [Permanent] do it: a 4xx Code means the agent rejected the
// request itself, except a 429 (rate limited), and a 5xx is a server-side
// failure worth retrying.
type ServiceError struct {
	// Code is the micro service error code (Nats-Service-Error-Code header),
	// 0 when the agent sent a non-numeric or absent code.
	Code int
	// Description is the human-readable error text (Nats-Service-Error header).
	Description string
	// ErrCode is the body's stable machine-readable code ("error"), such as
	// "rate_limited"; empty when the body is absent or not JSON.
	ErrCode string
	// Message is the body's human-readable detail ("message"), or Description
	// when the body has none (§9.1).
	Message string
	// RetryAfter is how long the agent asks the caller to wait before trying
	// again ("retry_after_s"), typically on a 429; 0 when it did not say.
	RetryAfter time.Duration
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

// ClientError reports whether the code is 4xx-class: the agent blames the
// request rather than itself. That is not the same as permanent — a 429 is a
// 4xx the same request may get past later. Use [Permanent] to decide on a retry.
func (e *ServiceError) ClientError() bool {
	return e.Code >= 400 && e.Code < 500
}

// QueryError is returned by [Stream.Text] when the agent pauses to ask the
// caller a question (§7). This client cannot answer, so the call ends here; it
// wraps [ErrQueryNotSupported], which [Permanent] reports as permanent, since
// sending the same prompt again gets the same question.
//
// The agent is not told: it waits for an answer until its own timeout, and then
// either fails or goes on with a default of its own choosing (§7.3). To get
// past the question, send a prompt that already answers it.
type QueryError struct {
	// ID is the query's correlation identifier.
	ID string
	// Prompt is the question the agent asked.
	Prompt string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("%s: %q", ErrQueryNotSupported.Error(), e.Prompt)
}

// Unwrap reports ErrQueryNotSupported so errors.Is(err, ErrQueryNotSupported)
// matches a *QueryError.
func (e *QueryError) Unwrap() error { return ErrQueryNotSupported }
