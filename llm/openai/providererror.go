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
	"strings"

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
		code, _ := apiErr.Code.(string)

		return &llm.ProviderError{
			Provider:              providerName,
			StatusCode:            apiErr.HTTPStatusCode,
			ContextLengthExceeded: isContextLengthError(code, apiErr.Type, apiErr.Message),
			Err:                   err,
		}
	}

	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) && reqErr.HTTPStatusCode > 0 {
		return &llm.ProviderError{
			Provider:              providerName,
			StatusCode:            reqErr.HTTPStatusCode,
			ContextLengthExceeded: isContextLengthError("", "", string(reqErr.Body)),
			Err:                   err,
		}
	}

	return err
}

// Error code and type by which OpenAI and llama.cpp report a prompt too long
// for the context window.
const (
	codeContextLengthExceeded = "context_length_exceeded"
	typeExceedContextSize     = "exceed_context_size_error"
)

// contextLengthPhrases are the error message fragments by which OpenAI and
// OpenAI-compatible servers report a prompt too long for the context window.
var contextLengthPhrases = []string{
	"maximum context length",             // OpenAI, vLLM
	codeContextLengthExceeded,            // OpenAI code, echoed by some proxies
	"exceeds the available context size", // llama.cpp
	typeExceedContextSize,                // llama.cpp type
	"context window",                     // LM Studio and others
	"prompt is too long",                 // proxies to Anthropic
}

// isContextLengthError reports whether an API error's code, type or message
// says the prompt does not fit the model's context window.
func isContextLengthError(code, typ, message string) bool {
	if code == codeContextLengthExceeded || typ == typeExceedContextSize {
		return true
	}

	message = strings.ToLower(message)

	for _, phrase := range contextLengthPhrases {
		if strings.Contains(message, phrase) {
			return true
		}
	}

	return false
}
