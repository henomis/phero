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

package agent_test

import (
	"context"
	"sync"
	"testing"

	"github.com/henomis/phero/v2/llm"
)

// appendMemory appends saved messages, as the memory backends do.
type appendMemory struct {
	mu    sync.Mutex
	msgs  []llm.Message
	saves int
}

func (m *appendMemory) Save(_ context.Context, msgs []llm.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.msgs = append(m.msgs, msgs...)
	m.saves++

	return nil
}

func (m *appendMemory) Retrieve(context.Context, string) ([]llm.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]llm.Message(nil), m.msgs...), nil
}

func (m *appendMemory) Clear(context.Context) error { return nil }

func (m *appendMemory) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.msgs)
}

func TestRun_CheckpointSavesEachIteration(t *testing.T) {
	mem := &appendMemory{}
	seen := map[int]int{}

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		seen[call] = mem.len()

		if call < 3 {
			return toolCallResult("echo", string(rune('a'+call)), "{}"), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)
	a.SetMemory(mem)
	a.SetCheckpoint(true)

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	// Before call 2: user, assistant, tool. Before call 3: two more.
	if seen[1] != 0 || seen[2] != 3 || seen[3] != 5 {
		t.Fatalf("memory sizes seen by calls = %v, want 0, 3, 5", seen)
	}

	// The final save adds only the answer: nothing is saved twice.
	if mem.len() != 6 || mem.saves != 3 {
		t.Fatalf("memory = %d messages in %d saves, want 6 in 3", mem.len(), mem.saves)
	}

	assertValidConversation(t, mem.msgs)
}

func TestRun_WithoutCheckpointSavesOnce(t *testing.T) {
	mem := &appendMemory{}
	seen := 0

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		seen += mem.len()

		if call < 3 {
			return toolCallResult("echo", string(rune('a'+call)), "{}"), nil
		}

		return textResult("done"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)
	a.SetMemory(mem)

	if _, err := a.Run(context.Background(), llm.Text("go")); err != nil {
		t.Fatal(err)
	}

	if seen != 0 || mem.len() != 6 || mem.saves != 1 {
		t.Fatalf("seen = %d, memory = %d messages in %d saves; want 0, 6, 1", seen, mem.len(), mem.saves)
	}
}

func TestRun_CheckpointThenResume(t *testing.T) {
	mem := &appendMemory{}
	ctx, cancel := context.WithCancel(context.Background())

	client := &scriptedLLM{fn: func(call int, _ []llm.Message) (*llm.Result, error) {
		if call == 2 {
			cancel() // the process "dies" during the second call
		}

		return toolCallResult("echo", string(rune('a'+call)), "{}"), nil
	}}

	a := mustNew(t, client, "agent", "desc")
	addEchoTool(t, a)
	a.SetMemory(mem)
	a.SetCheckpoint(true)

	if _, err := a.Run(ctx, llm.Text("go")); err == nil {
		t.Fatal("want the cancellation")
	}

	assertValidConversation(t, mem.msgs)

	// A run with no input continues from memory, with every pair intact.
	resume := &scriptedLLM{fn: func(int, []llm.Message) (*llm.Result, error) { return textResult("resumed"), nil }}
	b := mustNew(t, resume, "agent", "desc")
	b.SetMemory(mem)

	if res, err := b.Run(context.Background()); err != nil || res.TextContent() != "resumed" {
		t.Fatalf("resume = %v, %v", res, err)
	}

	assertValidConversation(t, resume.prompts[0])
}
