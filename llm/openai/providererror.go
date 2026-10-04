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

package openai

import (
	"errors"

	"github.com/sashabaranov/go-openai"

	"github.com/henomis/phero/v2/llm"
)

// providerName is the llm.ProviderError Provider value for this package.
const providerName = "openai"

// wrapAPIError wraps an error the API answered with an HTTP status in an
// *llm.ProviderError, and returns any other error unchanged. The SDK does not
// expose response headers, so RetryAfter is never set.
func wrapAPIError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatusCode > 0 {
		return &llm.ProviderError{Provider: providerName, StatusCode: apiErr.HTTPStatusCode, Err: err}
	}

	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) && reqErr.HTTPStatusCode > 0 {
		return &llm.ProviderError{Provider: providerName, StatusCode: reqErr.HTTPStatusCode, Err: err}
	}

	return err
}
