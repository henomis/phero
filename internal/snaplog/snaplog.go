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

// Package snaplog holds the cursor arithmetic memory backends share to
// implement memory.SnapshotStore over a log of messages that starts at an
// absolute position.
package snaplog

import (
	"fmt"
	"strconv"

	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
)

// Tail returns the messages of a log after snapshot (all of them without
// one), with their cursors. messages[0] is at absolute position offset.
// Leading tool results are left out: their tool call is evicted, pruned or
// summarized.
func Tail(messages []llm.Message, offset int, snapshot *memory.Snapshot) []memory.LoggedMessage {
	start := 0

	if snapshot != nil {
		if upTo, err := Position(snapshot.UpTo); err == nil {
			start = max(upTo+1-offset, 0)
		}
	}

	out := make([]memory.LoggedMessage, 0, len(messages)-min(start, len(messages)))

	for i := start; i < len(messages); i++ {
		if len(out) == 0 && messages[i].Role == llm.RoleTool {
			continue
		}

		out = append(out, memory.LoggedMessage{Message: messages[i], Cursor: Cursor(offset + i)})
	}

	return out
}

// View returns what Retrieve returns for a log with snapshot: the summary
// messages, then the messages after it.
func View(messages []llm.Message, offset int, snapshot memory.Snapshot) []llm.Message {
	tail := Tail(messages, offset, &snapshot)

	out := memory.SummaryMessages(snapshot)
	for _, lm := range tail {
		out = append(out, lm.Message)
	}

	return out
}

// Cutoff checks a SaveSnapshot call on a log whose next message will be at
// position next, and returns upTo's position: current must still be prev
// (memory.ErrSnapshotConflict), and upTo must name a message saved so far
// and move past current's (memory.ErrInvalidCursor).
func Cutoff(current, prev *memory.Snapshot, upTo memory.Cursor, next int) (int, error) {
	if !Same(current, prev) {
		return 0, memory.ErrSnapshotConflict
	}

	pos, err := Position(upTo)
	if err != nil || pos >= next {
		return 0, fmt.Errorf("%w: %q", memory.ErrInvalidCursor, upTo)
	}

	if current != nil {
		if prevPos, prevErr := Position(current.UpTo); prevErr == nil && pos <= prevPos {
			return 0, fmt.Errorf("%w: %q does not move past %q", memory.ErrInvalidCursor, upTo, current.UpTo)
		}
	}

	return pos, nil
}

// Cursor returns the cursor of the message at absolute position pos.
func Cursor(pos int) memory.Cursor {
	return memory.Cursor(strconv.Itoa(pos))
}

// Position returns the absolute position a cursor names.
func Position(c memory.Cursor) (int, error) {
	pos, err := strconv.Atoi(string(c))
	if err != nil || pos < 0 {
		return 0, fmt.Errorf("%w: %q", memory.ErrInvalidCursor, c)
	}

	return pos, nil
}

// Same reports whether a and b are the same stored snapshot, as SaveSnapshot
// compares prev: both nil, or both saved up to the same cursor.
func Same(a, b *memory.Snapshot) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.UpTo == b.UpTo
}
