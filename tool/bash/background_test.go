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
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// waitOutput reads a background shell until it stops running or the deadline
// passes, and returns all it read and the last result.
func waitOutput(t *testing.T, tool *Tool, id string, deadline time.Duration) (string, *OutputOutput) {
	t.Helper()

	var (
		got  strings.Builder
		last *OutputOutput
	)

	for end := time.Now().Add(deadline); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		out, err := tool.output(context.Background(), &OutputInput{BashID: id})
		if err != nil {
			t.Fatalf("output: %v", err)
		}

		got.WriteString(out.Output)

		last = out
		if !out.Running {
			break
		}
	}

	return got.String(), last
}

func startBackground(t *testing.T, tool *Tool, command string) string {
	t.Helper()

	out, err := tool.run(context.Background(), &Input{Command: command, RunInBackground: true})
	if err != nil {
		t.Fatalf("run %q: %v", command, err)
	}

	return out.BashID
}

func TestBackground_OutlivesForegroundTimeout(t *testing.T) {
	requireBash(t)

	tool, err := New(WithTimeout(100 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	id := startBackground(t, tool, "sleep 0.5; echo done")

	got, last := waitOutput(t, tool, id, 5*time.Second)
	if last.Running || !strings.Contains(got, "done") {
		t.Fatalf("output %q, running %v: want the command to finish", got, last.Running)
	}
}

func TestBackground_TimeoutOption(t *testing.T) {
	requireBash(t)

	tool, err := New(WithBackgroundTimeout(200 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	id := startBackground(t, tool, "sleep 5; echo done")

	got, last := waitOutput(t, tool, id, 4*time.Second)
	if last.Running || strings.Contains(got, "done") || time.Since(start) > 3*time.Second {
		t.Fatalf("output %q, running %v after %s: want the command killed at 200ms",
			got, last.Running, time.Since(start))
	}
}

func TestResolveBackgroundTimeout(t *testing.T) {
	tool, err := New(WithBackgroundTimeout(time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		ms   int
		want time.Duration
	}{
		{0, time.Minute},
		{1000, time.Second},
		{int(time.Hour / time.Millisecond), time.Minute}, // capped
	}

	for _, c := range cases {
		if got := tool.resolveBackgroundTimeout(c.ms); got != c.want {
			t.Errorf("resolveBackgroundTimeout(%d) = %s, want %s", c.ms, got, c.want)
		}
	}

	// No option: no limit, and the model's timeout is not capped by MaxTimeout.
	unlimited, err := New()
	if err != nil {
		t.Fatal(err)
	}

	if got := unlimited.resolveBackgroundTimeout(0); got != 0 {
		t.Errorf("default = %s, want no limit", got)
	}

	if got := unlimited.resolveBackgroundTimeout(int(time.Hour / time.Millisecond)); got != time.Hour {
		t.Errorf("1h = %s, want 1h", got)
	}
}

func TestClose_KillsShells(t *testing.T) {
	requireBash(t)

	tool, err := New()
	if err != nil {
		t.Fatal(err)
	}

	id := startBackground(t, tool, "sleep 30")

	tool.mu.RLock()
	shell := tool.shell[id]
	tool.mu.RUnlock()

	start := time.Now()

	if err = tool.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-shell.doneCh:
	case <-time.After(3 * time.Second):
		t.Fatal("shell still running 3s after Close")
	}

	if time.Since(start) > 3*time.Second {
		t.Fatalf("Close took %s", time.Since(start))
	}

	_, err = tool.run(context.Background(), &Input{Command: "true", RunInBackground: true})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("background after Close: err = %v, want ErrClosed", err)
	}

	// Foreground commands still run.
	if _, err = tool.run(context.Background(), &Input{Command: "true"}); err != nil {
		t.Fatalf("foreground after Close: %v", err)
	}
}

func TestBackground_OutputCap(t *testing.T) {
	requireBash(t)

	const capBytes = 1024

	tool, err := New(WithMaxBackgroundOutput(capBytes))
	if err != nil {
		t.Fatal(err)
	}

	id := startBackground(t, tool, "yes | head -c 10000")

	tool.mu.RLock()
	shell := tool.shell[id]
	tool.mu.RUnlock()

	<-shell.doneCh

	out, err := tool.output(context.Background(), &OutputInput{BashID: id})
	if err != nil {
		t.Fatal(err)
	}

	if out.Dropped != 10000-capBytes {
		t.Fatalf("dropped = %d, want %d", out.Dropped, 10000-capBytes)
	}

	note, body, found := strings.Cut(out.Output, "\n")
	if !found || note != "[... 8976 bytes dropped ...]" || len(body) != capBytes {
		t.Fatalf("note %q, body %d bytes: want the drop note and %d bytes", note, len(body), capBytes)
	}
}

func TestBackground_OutputCapAfterRead(t *testing.T) {
	shell := &backgroundShell{maxOut: 10}
	w := &lockedWriter{shell: shell}

	_, _ = w.Write([]byte("0123456789"))
	shell.readOffset = 6 // bash_output returned "012345"

	_, _ = w.Write([]byte("abcd")) // drops "0123": all read already

	if shell.unreadDropped != 0 || shell.readOffset != 2 || shell.out.String() != "456789abcd" {
		t.Fatalf("unread dropped %d, offset %d, out %q", shell.unreadDropped, shell.readOffset, shell.out.String())
	}

	_, _ = w.Write([]byte("efghij")) // drops "456789": "45" read, "6789" not

	if shell.unreadDropped != 4 || shell.readOffset != 0 || shell.out.String() != "abcdefghij" {
		t.Fatalf("unread dropped %d, offset %d, out %q", shell.unreadDropped, shell.readOffset, shell.out.String())
	}
}

func TestBackground_OutputCapKeepsUTF8Whole(t *testing.T) {
	shell := &backgroundShell{maxOut: 5}
	w := &lockedWriter{shell: shell}

	_, _ = w.Write([]byte("aé€b")) // 1+2+3+1 bytes: a 5-byte tail would split é

	if got := shell.out.String(); got != "€b" {
		t.Fatalf("out = %q, want %q", got, "€b")
	}
}
