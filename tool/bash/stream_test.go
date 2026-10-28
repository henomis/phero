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

package bash

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// chunkRecorder records each chunk an output handler gets, with its time.
type chunkRecorder struct {
	mu     sync.Mutex
	chunks []string
	times  []time.Time
}

func (r *chunkRecorder) handle(_ context.Context, chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.chunks = append(r.chunks, string(chunk))
	r.times = append(r.times, time.Now())
}

func (r *chunkRecorder) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return strings.Join(r.chunks, "")
}

func TestRun_OutputHandlerStreams(t *testing.T) {
	requireBash(t)

	var rec chunkRecorder

	tool, err := New(WithOutputHandler(rec.handle))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo a; sleep 0.5; echo b"})
	if err != nil {
		t.Fatal(err)
	}

	end := time.Now()

	if out.Output != "a\nb\n" {
		t.Fatalf("output = %q, want %q", out.Output, "a\nb\n")
	}

	if rec.joined() != "a\nb\n" {
		t.Fatalf("chunks = %q, want them to join to the output", rec.chunks)
	}

	// The first chunk arrived while the command was still sleeping.
	if rec.chunks[0] != "a\n" || end.Sub(rec.times[0]) < 300*time.Millisecond {
		t.Fatalf("first chunk %q came %s before the end, want \"a\\n\" well before it",
			rec.chunks[0], end.Sub(rec.times[0]))
	}
}

func TestRun_OutputHandlerOnTimeout(t *testing.T) {
	requireBash(t)

	var rec chunkRecorder

	tool, err := New(WithOutputHandler(rec.handle), WithTimeout(300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "echo started; sleep 5"})
	if err != nil {
		t.Fatal(err)
	}

	if !out.TimedOut || !strings.HasPrefix(out.Output, "started\n") {
		t.Fatalf("output = %+v, want a timeout with the output so far", out)
	}

	if rec.joined() != "started\n" {
		t.Fatalf("chunks = %q, want \"started\\n\"", rec.chunks)
	}
}

func TestRun_OutputHandlerPersistentShell(t *testing.T) {
	requireBash(t)

	var rec chunkRecorder

	tool, err := New(WithOutputHandler(rec.handle), WithPersistentShell())
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()

	if _, err = tool.run(context.Background(), &Input{Command: "cd " + dir}); err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{Command: "pwd"})
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(out.Output) != dir {
		t.Fatalf("pwd = %q, want %q", out.Output, dir)
	}

	// The shell's state report goes on its own pipe, never to the handler.
	if got := rec.joined(); got != dir+"\n" {
		t.Fatalf("chunks = %q, want only the pwd output", got)
	}
}

func TestRun_OutputHandlerStdoutAndStderr(t *testing.T) {
	requireBash(t)

	var (
		mu  sync.Mutex
		got bytes.Buffer
	)

	tool, err := New(WithOutputHandler(func(_ context.Context, chunk []byte) {
		mu.Lock()
		defer mu.Unlock()

		got.Write(chunk)
	}))
	if err != nil {
		t.Fatal(err)
	}

	out, err := tool.run(context.Background(), &Input{
		Command: "for i in $(seq 200); do echo out$i; echo err$i >&2; done",
	})
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if got.String() != out.Output {
		t.Fatalf("streamed %d bytes, output %d bytes: want the same text", got.Len(), len(out.Output))
	}

	if !strings.Contains(out.Output, "out200\n") || !strings.Contains(out.Output, "err200\n") {
		t.Fatalf("output misses lines: %q", out.Output[max(0, len(out.Output)-40):])
	}
}
