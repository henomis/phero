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
	"context"
	"iter"

	"google.golang.org/genai"

	"github.com/henomis/phero/v2/llm"
)

// Client streams responses via the Gemini streaming API.
var _ llm.StreamingLLM = (*Client)(nil)

// ExecuteStream calls the Gemini API in streaming mode, yielding text and
// thought deltas as they arrive while collecting every part, so the terminal
// chunk carries the same message Execute would return (thought signatures
// included), with the model name and token usage.
func (c *Client) ExecuteStream(
	ctx context.Context, messages []llm.Message, opts ...llm.CallOption,
) iter.Seq2[llm.StreamChunk, error] {
	return func(yield func(llm.StreamChunk, error) bool) {
		contents, config, err := c.buildRequest(messages, opts)
		if err != nil {
			yield(llm.StreamChunk{}, err)
			return
		}

		var col collector

		for res, streamErr := range c.client.Models.GenerateContentStream(ctx, c.model, contents, config) {
			if streamErr != nil {
				yield(llm.StreamChunk{}, wrapAPIError(streamErr))
				return
			}

			for _, p := range col.add(res) {
				if !yieldDelta(yield, p) {
					return
				}
			}
		}

		msg, err := col.message()
		if err != nil {
			yield(llm.StreamChunk{}, err)
			return
		}

		yieldFinal(yield, llm.StreamChunk{
			Done:    true,
			Message: msg,
			Model:   c.modelName(col.version),
			Usage:   usageFromGenai(col.usage),
		})
	}
}

// yieldFinal emits each assembled tool call, then the terminal chunk.
func yieldFinal(yield func(llm.StreamChunk, error) bool, final llm.StreamChunk) {
	for i := range final.Message.ToolCalls {
		tc := final.Message.ToolCalls[i]
		if !yield(llm.StreamChunk{ToolCall: &tc}, nil) {
			return
		}
	}

	yield(final, nil)
}

// collector gathers the parts, usage and model version of a streamed answer.
type collector struct {
	parts   []*genai.Part
	usage   *genai.GenerateContentResponseUsageMetadata
	version string
	last    *genai.GenerateContentResponse
}

// add records one streamed response and returns its new parts.
func (c *collector) add(res *genai.GenerateContentResponse) []*genai.Part {
	c.last = res

	if res.UsageMetadata != nil {
		c.usage = res.UsageMetadata
	}

	if res.ModelVersion != "" {
		c.version = res.ModelVersion
	}

	if len(res.Candidates) == 0 || res.Candidates[0] == nil || res.Candidates[0].Content == nil {
		return nil
	}

	var added []*genai.Part

	for _, p := range res.Candidates[0].Content.Parts {
		if p != nil {
			added = append(added, p)
		}
	}

	c.parts = append(c.parts, added...)

	return added
}

// message assembles the collected parts. Without any, it reports why: a
// blocked prompt or answer, or a stream that ended early.
func (c *collector) message() (*llm.Message, error) {
	if len(c.parts) > 0 {
		return messageFromParts(c.parts), nil
	}

	if _, err := candidateParts(c.last); err != nil {
		return nil, err
	}

	return nil, &IncompleteStreamError{}
}

// yieldDelta yields the text or thought carried by a streamed part, if any. It
// reports whether the consumer wants more.
func yieldDelta(yield func(llm.StreamChunk, error) bool, p *genai.Part) bool {
	switch {
	case p.FunctionCall != nil || p.Text == "":
		return true
	case p.Thought:
		return yield(llm.StreamChunk{ReasoningDelta: p.Text}, nil)
	default:
		return yield(llm.StreamChunk{TextDelta: p.Text}, nil)
	}
}
