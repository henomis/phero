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

//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/henomis/phero/v2/agent"
	"github.com/henomis/phero/v2/llm"
)

// compactionChunks is how many chunks the long-run test asks the agent to read,
// one tool call each. Override with E2E_COMPACTION_CHUNKS.
const compactionChunks = 40

// TestAgent_LongRunCompaction drives an agent through many tool-calling
// iterations whose results together exceed the model's context window, and
// checks that compaction keeps the run alive: no context-length error, at
// least one compaction, and the run reaches the last chunk.
//
// Run it against a local model with a small context, for example llama.cpp
// with -c 8192: OPENAI_BASE_URL=http://localhost:8080/v1 OPENAI_MODEL=MiniCPM5-2B.
func TestAgent_LongRunCompaction(t *testing.T) {
	chunks := compactionChunks
	if v, err := strconv.Atoi(envOr("E2E_COMPACTION_CHUNKS", "")); err == nil && v > 0 {
		chunks = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	client := buildOpenAILLM()

	a, err := agent.New(client, "reader", fmt.Sprintf(
		"You read a document chunk by chunk with the read_chunk tool. Start at chunk 1. "+
			"After each result, call read_chunk with the next chunk number. "+
			"Do not stop until you have read chunk %d, then reply with the word DONE.", chunks))
	if err != nil {
		t.Fatal(err)
	}

	var highest atomic.Int64

	type readInput struct {
		N int `json:"n" jsonschema:"description=The chunk number to read"`
	}

	type readOutput struct {
		Text string `json:"text"`
	}

	tool, err := llm.NewTool("read_chunk", "Read one chunk of the document.",
		func(_ context.Context, in *readInput) (*readOutput, error) {
			if int64(in.N) > highest.Load() {
				highest.Store(int64(in.N))
			}

			// About 600 tokens per chunk: a dozen chunks fill an 8k context.
			body := strings.Repeat(fmt.Sprintf("Chunk %d is filler text about nothing in particular. ", in.N), 50)

			return &readOutput{Text: fmt.Sprintf("%s\nNext: call read_chunk with n=%d.", body, in.N+1)}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	if err = a.AddTool(tool); err != nil {
		t.Fatal(err)
	}

	var compactions atomic.Int32

	summarizer := agent.NewSummaryCompactor(client, agent.WithKeepRecent(6))
	a.SetCompaction(agent.CompactorFunc(func(ctx context.Context, msgs []llm.Message) ([]llm.Message, error) {
		compactions.Add(1)
		t.Logf("compacting %d messages (highest chunk %d)", len(msgs), highest.Load())

		return summarizer.Compact(ctx, msgs)
	}), 5000)
	a.SetMaxIterations(chunks * 3)

	res, err := a.Run(ctx, llm.Text("Read the document."))
	if err != nil {
		t.Fatalf("Run: %v (highest chunk %d, compactions %d)", err, highest.Load(), compactions.Load())
	}

	t.Logf("highest chunk %d, compactions %d, iterations %d, answer %q",
		highest.Load(), compactions.Load(), res.Summary.Iterations, res.TextContent())

	if compactions.Load() == 0 {
		t.Fatal("no compaction happened: the run never got near the context limit")
	}

	if highest.Load() < int64(chunks) {
		t.Fatalf("the agent stopped at chunk %d of %d", highest.Load(), chunks)
	}
}
