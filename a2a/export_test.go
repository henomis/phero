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

// This file exposes internal symbols for white-box testing.
// It is part of package a2a (not package a2a_test) so it can access unexported names.

package a2a

import (
	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/henomis/phero/agent"
	"github.com/henomis/phero/llm"
)

var (
	TranslatePartsToPhero = translatePartsToPhero
	TranslateResultToA2A  = translateResultToA2A
	ExtractTextFromResult = extractTextFromResult
	ExtractStatusMessage  = extractStatusMessage
	SanitizeToolName      = sanitizeToolName
)

// RESTPathPrefix exposes the constant for tests that verify URL construction.
const RESTPathPrefix = restPathPrefix

// MakeAgentResult builds an *agent.Result from a slice of ContentParts for
// use in message translation tests.
func MakeAgentResult(parts []llm.ContentPart) *agent.Result {
	return &agent.Result{Parts: parts}
}

// MakeA2ATextMessage wraps sdka2a.NewMessage for test convenience.
var MakeA2ATextMessage = func(text string) *sdka2a.Message {
	return sdka2a.NewMessage(sdka2a.MessageRoleAgent, sdka2a.NewTextPart(text))
}
