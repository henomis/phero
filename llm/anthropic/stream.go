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
	"context"
	"iter"
	"strings"

	anthropicapi "github.com/anthropics/anthropic-sdk-go"

	"github.com/henomis/phero/llm"
)

// Client streams responses via the Anthropic Messages streaming API.
var _ llm.StreamingLLM = (*Client)(nil)

// ExecuteStream calls the Anthropic Messages API in streaming mode, yielding text
// and reasoning deltas as they arrive while accumulating the full message.
//
// The terminal chunk (Done == true) carries the complete assistant message, the
// model name, and token usage (including cache token counts).
func (c *Client) ExecuteStream(
	ctx context.Context, messages []llm.Message, tools []*llm.Tool,
) iter.Seq2[llm.StreamChunk, error] {
	return func(yield func(llm.StreamChunk, error) bool) {
		params, err := c.buildParams(messages, tools)
		if err != nil {
			yield(llm.StreamChunk{}, err)
			return
		}

		stream := c.client.Messages.NewStreaming(ctx, params)
		defer func() { _ = stream.Close() }()

		var acc anthropicapi.Message

		for stream.Next() {
			event := stream.Current()
			if accErr := acc.Accumulate(event); accErr != nil {
				yield(llm.StreamChunk{}, accErr)
				return
			}

			if event.Type == "content_block_delta" {
				switch {
				case event.Delta.Text != "":
					if !yield(llm.StreamChunk{TextDelta: event.Delta.Text}, nil) {
						return
					}
				case event.Delta.Thinking != "":
					if !yield(llm.StreamChunk{ReasoningDelta: event.Delta.Thinking}, nil) {
						return
					}
				}
			}
		}

		if streamErr := stream.Err(); streamErr != nil {
			yield(llm.StreamChunk{}, streamErr)
			return
		}

		msg, err := messageFromAnthropic(&acc)
		if err != nil {
			yield(llm.StreamChunk{}, err)
			return
		}

		// Emit each assembled tool call before the terminal chunk.
		for i := range msg.ToolCalls {
			tc := msg.ToolCalls[i]
			if !yield(llm.StreamChunk{ToolCall: &tc}, nil) {
				return
			}
		}

		model := string(acc.Model)
		if model == "" {
			model = strings.TrimSpace(c.model)
		}

		yield(llm.StreamChunk{
			Done:    true,
			Message: msg,
			Model:   model,
			Usage: &llm.Usage{
				InputTokens:      int(acc.Usage.InputTokens),
				OutputTokens:     int(acc.Usage.OutputTokens),
				CacheReadTokens:  int(acc.Usage.CacheReadInputTokens),
				CacheWriteTokens: int(acc.Usage.CacheCreationInputTokens),
			},
		}, nil)
	}
}
