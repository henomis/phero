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

package google

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/henomis/phero/v2/llm"
)

// providerName is the llm.ProviderError Provider value for this package.
const providerName = "google"

// retryInfoType is the type URL of the google.rpc.RetryInfo error detail, by
// which the Gemini API says how long to wait after a 429.
const retryInfoType = "type.googleapis.com/google.rpc.RetryInfo"

// wrapAPIError wraps an error the API answered with an HTTP status in an
// *llm.ProviderError, with the wait the response asked for, and returns any
// other error unchanged.
func wrapAPIError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr genai.APIError
	if !errors.As(err, &apiErr) || apiErr.Code == 0 {
		return err
	}

	return &llm.ProviderError{
		Provider:              providerName,
		StatusCode:            apiErr.Code,
		RetryAfter:            retryAfter(apiErr.Details),
		ContextLengthExceeded: isContextLengthError(apiErr.Code, apiErr.Message),
		Err:                   err,
	}
}

// isContextLengthError reports whether an API error says the prompt has more
// tokens than the model accepts: a 400 "The input token count (N) exceeds the
// maximum number of tokens allowed (M)".
func isContextLengthError(status int, message string) bool {
	message = strings.ToLower(message)

	return status == http.StatusBadRequest &&
		strings.Contains(message, "token count") && strings.Contains(message, "exceeds the maximum")
}

// retryAfter reads the retryDelay of a google.rpc.RetryInfo detail ("30s",
// "1.5s"). It returns 0 when there is none, or it says nonsense.
func retryAfter(details []map[string]any) time.Duration {
	for _, d := range details {
		if t, _ := d["@type"].(string); t != retryInfoType {
			continue
		}

		delay, _ := d["retryDelay"].(string)

		if wait, err := time.ParseDuration(delay); err == nil && wait > 0 {
			return wait
		}
	}

	return 0
}
