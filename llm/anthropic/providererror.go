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

package anthropic

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"

	"github.com/henomis/phero/llm"
)

// providerName is the llm.ProviderError Provider value for this package.
const providerName = "anthropic"

// wrapAPIError wraps an error the API answered with an HTTP status in an
// *llm.ProviderError, with the wait the response asked for, and returns any
// other error unchanged.
func wrapAPIError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr *anthropicapi.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode == 0 {
		return err
	}

	pe := &llm.ProviderError{Provider: providerName, StatusCode: apiErr.StatusCode, Err: err}
	if apiErr.Response != nil {
		pe.RetryAfter = retryAfter(apiErr.Response.Header, time.Now())
	}

	return pe
}

// retryAfter reads how long a response asks the caller to wait: retry-after-ms
// (Anthropic's own, in milliseconds) first, then the standard Retry-After, in
// seconds or as an HTTP date. It returns 0 when neither says, or says nonsense.
func retryAfter(h http.Header, now time.Time) time.Duration {
	if v := h.Get("Retry-After-Ms"); v != "" {
		if ms, err := strconv.ParseFloat(v, 64); err == nil && ms > 0 {
			return time.Duration(ms * float64(time.Millisecond))
		}
	}

	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}

	if s, err := strconv.ParseFloat(v, 64); err == nil {
		if s > 0 {
			return time.Duration(s * float64(time.Second))
		}

		return 0
	}

	if at, err := http.ParseTime(v); err == nil && at.After(now) {
		return at.Sub(now)
	}

	return 0
}
