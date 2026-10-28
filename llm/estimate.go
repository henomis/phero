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

package llm

import "strings"

// Constants of EstimateTokens.
const (
	charsPerToken         = 4
	messageOverheadTokens = 4
	imageTokens           = 1000
)

// EstimateTokens returns a rough count of the tokens messages take in a
// prompt: about four characters per token of text and tool call arguments, a
// few tokens per message, and a flat count per image. Use it before a
// provider has reported a prompt size; the agent decides on compaction with
// it at the start of a run, after a compaction, or when the provider reports
// no usage.
func EstimateTokens(messages []Message) int {
	chars, images := 0, 0

	for _, m := range messages {
		for _, p := range m.Parts {
			if p.Type == ContentTypeImageURL || p.Type == ContentTypeImageBase64 {
				images++
				continue
			}

			chars += len(p.Text)
		}

		for _, tc := range m.ToolCalls {
			chars += len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}

	return chars/charsPerToken + len(messages)*messageOverheadTokens + images*imageTokens
}

// MaxToolNameLength is the longest tool name every provider accepts.
const MaxToolNameLength = 64

// SanitizeToolName maps name to a tool name every provider accepts: each
// character outside [a-zA-Z0-9_-] becomes '_', and the result is cut to
// MaxToolNameLength. It is empty only for an empty name.
func SanitizeToolName(name string) string {
	s := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}

		return '_'
	}, name)

	return s[:min(len(s), MaxToolNameLength)]
}
