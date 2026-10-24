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

package middleware

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/henomis/phero/v2/llm"
)

// streamStub streams its text in two deltas and a terminal chunk. Before each
// stream, failures (one per call, popped in order) can fail the call before
// or after its first chunk.
type streamStub struct {
	text     string
	calls    int
	failures []streamFailure
}

type streamFailure struct {
	err        error
	afterChunk bool
}

func (s *streamStub) Execute(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
	msg := llm.AssistantMessage([]llm.ContentPart{llm.Text(s.text)})
	return &llm.Result{Message: &msg}, nil
}

func (s *streamStub) ExecuteStream(context.Context, []llm.Message, ...llm.CallOption) iter.Seq2[llm.StreamChunk, error] {
	s.calls++

	var failure streamFailure
	if len(s.failures) > 0 {
		failure, s.failures = s.failures[0], s.failures[1:]
	}

	return func(yield func(llm.StreamChunk, error) bool) {
		if failure.err != nil && !failure.afterChunk {
			yield(llm.StreamChunk{}, failure.err)
			return
		}

		half := len(s.text) / 2
		if !yield(llm.StreamChunk{TextDelta: s.text[:half]}, nil) {
			return
		}

		if failure.err != nil {
			yield(llm.StreamChunk{}, failure.err)
			return
		}

		if !yield(llm.StreamChunk{TextDelta: s.text[half:]}, nil) {
			return
		}

		msg := llm.AssistantMessage([]llm.ContentPart{llm.Text(s.text)})
		yield(llm.StreamChunk{Done: true, Message: &msg}, nil)
	}
}

// collect drains a stream: the deltas, the terminal chunk count and the error.
func collect(t *testing.T, client llm.LLM) (deltas []string, done int, err error) {
	t.Helper()

	s, ok := client.(llm.StreamingLLM)
	if !ok {
		t.Fatalf("%T does not stream", client)
	}

	for chunk, chunkErr := range s.ExecuteStream(context.Background(), nil) {
		if chunkErr != nil {
			return deltas, done, chunkErr
		}

		if chunk.TextDelta != "" {
			deltas = append(deltas, chunk.TextDelta)
		}

		if chunk.Done {
			done++
		}
	}

	return deltas, done, nil
}

var errUnavailable = &llm.ProviderError{Provider: "p", StatusCode: 503, Err: errors.New("unavailable")}

func TestRetryStream_RetriesBeforeFirstChunk(t *testing.T) {
	inner := &streamStub{text: "hello", failures: []streamFailure{{err: errUnavailable}}}

	mw, err := NewRetry(3, WithInitialBackoff(0))
	if err != nil {
		t.Fatal(err)
	}

	deltas, done, err := collect(t, llm.Use(inner, mw))
	if err != nil || strings.Join(deltas, "") != "hello" || done != 1 || inner.calls != 2 {
		t.Fatalf("deltas = %v, done = %d, err = %v, calls = %d", deltas, done, err, inner.calls)
	}
}

func TestRetryStream_NoRetryAfterFirstChunk(t *testing.T) {
	inner := &streamStub{text: "hello", failures: []streamFailure{{err: errUnavailable, afterChunk: true}}}

	mw, err := NewRetry(3, WithInitialBackoff(0))
	if err != nil {
		t.Fatal(err)
	}

	deltas, _, err := collect(t, llm.Use(inner, mw))
	if !errors.Is(err, errUnavailable) || len(deltas) != 1 || inner.calls != 1 {
		t.Fatalf("deltas = %v, err = %v, calls = %d; want one delta then the error", deltas, err, inner.calls)
	}
}

func TestLimiterStream_ReleasesSlotAtEnd(t *testing.T) {
	mw, stop, err := NewLimiter(1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	client := llm.Use(&streamStub{text: "hello"}, mw)

	// With one slot, a second stream would block forever if the first kept it.
	for range 2 {
		if deltas, done, streamErr := collect(t, client); streamErr != nil || len(deltas) != 2 || done != 1 {
			t.Fatalf("deltas = %v, done = %d, err = %v", deltas, done, streamErr)
		}
	}
}

func TestGuardrailsStream_ChecksResultAtEnd(t *testing.T) {
	mw := NewGuardrails(WithResultGuard("no-secrets", func(_ context.Context, r *llm.Result) error {
		if strings.Contains(r.Message.TextContent(), "secret") {
			return errors.New("leak")
		}

		return nil
	}))

	if deltas, done, err := collect(t, llm.Use(&streamStub{text: "hello"}, mw)); err != nil || len(deltas) != 2 || done != 1 {
		t.Fatalf("clean stream: deltas = %v, done = %d, err = %v", deltas, done, err)
	}

	_, done, err := collect(t, llm.Use(&streamStub{text: "my secret"}, mw))
	if !errors.Is(err, llm.ErrGuardrailBlocked) || done != 0 {
		t.Fatalf("blocked stream: done = %d, err = %v; want the guardrail error instead of the result", done, err)
	}
}

func TestMiddlewareStream_WrapsBufferedLLM(t *testing.T) {
	mw, err := NewRetry(2)
	if err != nil {
		t.Fatal(err)
	}

	deltas, done, err := collect(t, llm.Use(okLLM("buffered"), mw))
	if err != nil || strings.Join(deltas, "") != "buffered" || done != 1 {
		t.Fatalf("deltas = %v, done = %d, err = %v", deltas, done, err)
	}
}
