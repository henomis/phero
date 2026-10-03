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
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	natsio "github.com/nats-io/nats.go/micro"

	"github.com/henomis/phero/llm"
)

// Machine-readable codes (§9.1) the server sends for a failed run.
const (
	errCodeInternal      = "internal_error"
	errCodeLLMAuthFailed = "llm_auth_failed"
	errCodeLLMRejected   = "llm_rejected"
	errCodeLLMError      = "llm_error"

	errCodeBadRequest   = "bad_request"
	errCodeUnauthorized = "unauthorized"
	errCodeForbidden    = "forbidden"
	errCodeNotFound     = "not_found"
	errCodeConflict     = "conflict"
	errCodeRateLimited  = "rate_limited"
)

// defaultErrCodes names each §9.2 code, for a CodedError that sets none. The
// keys are also the only codes the server sends: §9.2 says agents MUST use
// codes from its table.
var defaultErrCodes = map[int]string{
	http.StatusBadRequest:          errCodeBadRequest,
	http.StatusUnauthorized:        errCodeUnauthorized,
	http.StatusForbidden:           errCodeForbidden,
	http.StatusNotFound:            errCodeNotFound,
	http.StatusConflict:            errCodeConflict,
	http.StatusTooManyRequests:     errCodeRateLimited,
	http.StatusInternalServerError: errCodeInternal,
}

// errorFor chooses the error sent for a run that failed with runErr: the
// configured mapper first, then a CodedError in runErr's chain, then a run cut
// short by a drain, then the mapping of LLM provider errors, then 500.
//
// ctx is the context the run was given; it is only ever cancelled by a drain
// that ran out of budget.
func (s *Server) errorFor(ctx context.Context, runErr error) *CodedError {
	if s.cfg.errorMapper != nil {
		if ce := s.cfg.errorMapper(runErr); ce != nil {
			return ce
		}
	}

	if ce, ok := errors.AsType[*CodedError](runErr); ok {
		return ce
	}

	if ctx.Err() != nil && errors.Is(runErr, context.Canceled) {
		return &CodedError{
			Code: http.StatusInternalServerError, ErrCode: errCodeServerDraining, Message: drainingMessage, Err: runErr,
		}
	}

	if ce := providerErrorFor(runErr); ce != nil {
		return ce
	}

	return &CodedError{Code: http.StatusInternalServerError, ErrCode: errCodeInternal, Err: runErr}
}

// providerErrorFor maps an LLM provider's error to the error the caller
// receives, or returns nil when runErr carries none.
//
// Only a rate limit keeps its code, because only there does the provider's
// verdict carry over to the caller: the agent is busy, and the caller may retry
// later. A rejected API key is the agent's configuration, not the caller's
// credentials, so a 401/403 would blame the wrong party. A rejected request may
// be the caller's prompt or the agent's own tool schema, and a wrong 400 stops
// callers from retrying a fault that is not theirs. Both are sent as 500 with a
// code that says what happened; a handler that knows better returns a
// CodedError or configures WithErrorMapper.
func providerErrorFor(runErr error) *CodedError {
	pe, ok := errors.AsType[*llm.ProviderError](runErr)
	if !ok {
		return nil
	}

	switch pe.StatusCode {
	case http.StatusTooManyRequests:
		return &CodedError{
			Code: http.StatusTooManyRequests, ErrCode: errCodeRateLimited,
			RetryAfter: pe.RetryAfter, Err: runErr,
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &CodedError{Code: http.StatusInternalServerError, ErrCode: errCodeLLMAuthFailed, Err: runErr}
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return &CodedError{Code: http.StatusInternalServerError, ErrCode: errCodeLLMRejected, Err: runErr}
	default:
		return &CodedError{Code: http.StatusInternalServerError, ErrCode: errCodeLLMError, Err: runErr}
	}
}

// sendCodedError sends ce as a service error (§9.1) followed by the terminator
// (§9.3), normalising it to what the protocol allows: a code outside the §9.2
// table becomes 500, and an empty ErrCode or Message is filled in.
func sendCodedError(req natsio.Request, ce *CodedError) {
	code := ce.Code
	if _, ok := defaultErrCodes[code]; !ok {
		code = http.StatusInternalServerError
	}

	errCode := ce.ErrCode
	if errCode == "" {
		errCode = defaultErrCodes[code]
	}

	message := ce.Message
	if message == "" && ce.Err != nil {
		message = ce.Err.Error()
	}

	if message == "" {
		message = http.StatusText(code)
	}

	sendErrorBody(req, strconv.Itoa(code), headerSafe(message), encodeErrorBody(errCode, message, ce.RetryAfter))
}

// headerSafe makes s usable as the Nats-Service-Error header value. A header
// cannot hold a line break — it would end the header there and corrupt the
// message — and micro refuses an empty description, sending no error at all,
// which the caller would read as an empty answer.
func headerSafe(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return http.StatusText(http.StatusInternalServerError)
	}

	return s
}

// retryAfterSeconds renders d as the whole seconds of retry_after_s, rounded
// up so a caller never comes back early, and 0 (omitted) when d is not positive.
func retryAfterSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}

	return int64(math.Ceil(d.Seconds()))
}
