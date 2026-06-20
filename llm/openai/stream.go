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
	"context"
	"errors"
	"io"
	"iter"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/henomis/phero/llm"
)

// Client streams responses via the Chat Completions streaming API.
var _ llm.StreamingLLM = (*Client)(nil)

// ExecuteStream calls the Chat Completions API in streaming mode, yielding text
// deltas as they arrive and assembling tool calls from their incremental deltas.
//
// The terminal chunk (Done == true) carries the complete assistant message, the
// model name, and — when the server reports it — token usage.
func (c *Client) ExecuteStream(
	ctx context.Context, messages []llm.Message, tools []*llm.Tool,
) iter.Seq2[llm.StreamChunk, error] {
	return func(yield func(llm.StreamChunk, error) bool) {
		request := openai.ChatCompletionRequest{
			Model:         c.model,
			Messages:      messagesToOpenAI(messages),
			Temperature:   c.temperature,
			Stream:        true,
			StreamOptions: &openai.StreamOptions{IncludeUsage: true},
		}
		if len(tools) > 0 {
			request.Tools = c.openaiTools(tools)
		}

		stream, err := c.client.CreateChatCompletionStream(ctx, request)
		if err != nil {
			yield(llm.StreamChunk{}, err)
			return
		}
		defer func() { _ = stream.Close() }()

		var (
			textBuilder strings.Builder
			toolCalls   = map[int]*llm.ToolCall{}
			toolOrder   []int
			usage       *llm.Usage
			model       = c.model
		)

		for {
			response, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				break
			}

			if recvErr != nil {
				yield(llm.StreamChunk{}, recvErr)
				return
			}

			if response.Model != "" {
				model = response.Model
			}

			if response.Usage != nil {
				usage = &llm.Usage{
					InputTokens:  response.Usage.PromptTokens,
					OutputTokens: response.Usage.CompletionTokens,
				}
			}

			if len(response.Choices) == 0 {
				continue
			}

			delta := response.Choices[0].Delta

			if delta.ReasoningContent != "" {
				if !yield(llm.StreamChunk{ReasoningDelta: delta.ReasoningContent}, nil) {
					return
				}
			}

			if delta.Content != "" {
				textBuilder.WriteString(delta.Content)

				if !yield(llm.StreamChunk{TextDelta: delta.Content}, nil) {
					return
				}
			}

			for _, tc := range delta.ToolCalls {
				idx := 0
				if tc.Index != nil {
					idx = *tc.Index
				}

				existing, ok := toolCalls[idx]
				if !ok {
					existing = &llm.ToolCall{Type: llm.ToolTypeFunction}
					toolCalls[idx] = existing
					toolOrder = append(toolOrder, idx)
				}

				if tc.ID != "" {
					existing.ID = tc.ID
				}

				if tc.Type != "" {
					existing.Type = string(tc.Type)
				}

				if tc.Function.Name != "" {
					existing.Function.Name = tc.Function.Name
				}

				existing.Function.Arguments += tc.Function.Arguments
			}
		}

		// Emit each assembled tool call, then the terminal chunk with the full message.
		assembled := make([]llm.ToolCall, 0, len(toolOrder))
		for _, idx := range toolOrder {
			tc := *toolCalls[idx]

			assembled = append(assembled, tc)
			if !yield(llm.StreamChunk{ToolCall: &tc}, nil) {
				return
			}
		}

		message := &llm.Message{Role: llm.RoleAssistant, ToolCalls: assembled}
		if text := textBuilder.String(); text != "" {
			message.Parts = []llm.ContentPart{llm.Text(text)}
		}

		yield(llm.StreamChunk{Done: true, Message: message, Usage: usage, Model: model}, nil)
	}
}
