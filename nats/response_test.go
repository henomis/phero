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

package nats

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
)

// textHandler answers every prompt with a fixed text, without an LLM.
type textHandler string

func (h textHandler) Run(_ context.Context, _ ...llm.ContentPart) (*agent.Result, error) {
	return &agent.Result{Parts: []llm.ContentPart{llm.Text(string(h))}}, nil
}

// escapeHeavyText is about size bytes of text that JSON escaping inflates
// (HTML characters, quotes, control characters) mixed with multi-byte runes.
func escapeHeavyText(size int) string {
	const unit = `<a href="x">é & 日本</a>` + "\n\t\x01"

	return strings.Repeat(unit, size/len(unit)+1)
}

// responseText decodes the response chunks of a recorded stream, in order, and
// fails on anything but status and response chunks before the terminator.
func responseText(t *testing.T, responses [][]byte) string {
	t.Helper()

	if len(responses) == 0 || len(responses[len(responses)-1]) != 0 {
		t.Fatalf("stream does not end with the zero-byte terminator")
	}

	var sb strings.Builder

	for _, msg := range responses[:len(responses)-1] {
		var chunk rawChunk
		if err := json.Unmarshal(msg, &chunk); err != nil {
			t.Fatalf("chunk is not JSON: %v", err)
		}

		switch chunk.Type {
		case chunkTypeStatus:
		case chunkTypeResponse:
			sb.WriteString(decodeResponseText(chunk.Data))
		default:
			t.Fatalf("unexpected chunk type %q", chunk.Type)
		}
	}

	return sb.String()
}

func TestEncodeResponseChunks_SmallTextIsOneChunk(t *testing.T) {
	chunks, ok := encodeResponseChunks("hello", 1024)
	if !ok || len(chunks) != 1 {
		t.Fatalf("chunks = %d, ok = %v; want 1 chunk", len(chunks), ok)
	}

	if string(chunks[0]) != string(encodeResponseChunk("hello")) {
		t.Fatalf("chunk = %s, want the single-chunk encoding", chunks[0])
	}
}

// TestEncodeResponseChunks_SplitsUnderTheLimit verifies that every encoded
// chunk fits, that each carries whole runes, and that the text survives the
// split, for text that escaping inflates up to six times.
func TestEncodeResponseChunks_SplitsUnderTheLimit(t *testing.T) {
	text := escapeHeavyText(200_000)

	for _, limit := range []int{64, 1000, 64 * 1024} {
		chunks, ok := encodeResponseChunks(text, limit)
		if !ok {
			t.Fatalf("limit %d: encodeResponseChunks reported no fit", limit)
		}

		var sb strings.Builder

		for i, b := range chunks {
			if len(b) > limit {
				t.Fatalf("limit %d: chunk %d is %d bytes", limit, i, len(b))
			}

			var chunk rawChunk
			if err := json.Unmarshal(b, &chunk); err != nil || chunk.Type != chunkTypeResponse {
				t.Fatalf("limit %d: chunk %d is not a response chunk: %s", limit, i, b)
			}

			piece := decodeResponseText(chunk.Data)
			if !utf8.ValidString(piece) {
				t.Fatalf("limit %d: chunk %d splits a rune", limit, i)
			}

			sb.WriteString(piece)
		}

		if sb.String() != text {
			t.Fatalf("limit %d: rejoined text differs from the original", limit)
		}
	}
}

func TestEncodeResponseChunks_NoRuneFits(t *testing.T) {
	overhead := len(encodeResponseChunk(""))

	if _, ok := encodeResponseChunks("<<<", overhead+5); ok {
		t.Fatal("a limit below one escaped rune must report no fit")
	}
}

// TestProcessPrompt_LargeAnswerIsChunked verifies that an answer bigger than
// max_payload reaches the caller whole, instead of being dropped (issue #1).
func TestProcessPrompt_LargeAnswerIsChunked(t *testing.T) {
	text := escapeHeavyText(3 << 20)

	s, ctx := newTestServer(t, time.Second)
	s.handler = textHandler(text)

	// The broker rejects anything above max_payload, as a real one does.
	req := &fakeRequest{data: []byte("write a long report"), maxPayload: s.maxPublishBytes()}
	s.processPrompt(ctx, req)

	if req.errCode != "" {
		t.Fatalf("unexpected service error %s: %s", req.errCode, req.errBody)
	}

	if got := responseText(t, req.responses); got != text {
		t.Fatalf("caller read %d bytes, want %d", len(got), len(text))
	}
}

// TestProcessPrompt_FailedPublishIsAnError verifies that an answer the broker
// rejects is reported as an error followed by the terminator (§9.3), not as an
// empty answer.
func TestProcessPrompt_FailedPublishIsAnError(t *testing.T) {
	s, ctx := newTestServer(t, time.Second)
	s.handler = textHandler(strings.Repeat("x", 10_000))

	// The broker allows less than the connection claims: the ack fits, the
	// answer does not.
	req := &fakeRequest{data: []byte("hi"), maxPayload: 100}
	s.processPrompt(ctx, req)

	if req.errCode != "500" {
		t.Fatalf("error code = %q, want 500", req.errCode)
	}

	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(req.errBody, &body); err != nil || body.Error != errCodeResponseTooLarge {
		t.Fatalf("error body = %s, want code %q", req.errBody, errCodeResponseTooLarge)
	}

	last := req.responses[len(req.responses)-1]
	if len(last) != 0 {
		t.Fatalf("stream does not end with the terminator after the error")
	}
}
