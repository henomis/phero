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

package memory

import "github.com/henomis/phero/v2/llm"

// SplitPoint returns where to split messages so that messages[:j] can be
// summarized or dropped and messages[j:] kept, at or after the wanted index i.
//
// Providers reject a tool result whose tool call is not in the conversation,
// so the kept part must not start with one: j moves forward past the tool
// results at i, keeping an assistant message and the results of its tool
// calls on the same side. The result is clamped to [0, len(messages)].
func SplitPoint(messages []llm.Message, i int) int {
	i = max(0, min(i, len(messages)))

	for i < len(messages) && messages[i].Role == llm.RoleTool {
		i++
	}

	return i
}

// TrimOrphanToolResults returns messages without its leading tool results,
// which answer tool calls no longer in the slice, for example after the
// oldest messages were evicted. messages is returned as is when it does not
// start with one.
func TrimOrphanToolResults(messages []llm.Message) []llm.Message {
	return messages[SplitPoint(messages, 0):]
}
