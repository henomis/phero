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

package memory

import (
	"context"

	"github.com/henomis/phero/v2/llm"
)

// Snapshot is a summary that stands for the oldest messages of a memory's
// log. The log keeps those messages (unless the backend prunes them); a
// SnapshotStore's Retrieve and LoadSnapshot return the snapshot in their
// place.
type Snapshot struct {
	// Summary is the summary text.
	Summary string `json:"summary"`
	// Pinned are messages kept verbatim with the summary, such as the user
	// message that set the task.
	Pinned []llm.Message `json:"pinned,omitempty"`
	// UpTo is the cursor of the last log message the snapshot stands for. The
	// store sets it when it saves or loads the snapshot.
	UpTo Cursor `json:"upTo"`
}

// Cursor is the position of a message in a memory's log. It is opaque: only
// the memory that returned it understands it, and it stays valid while older
// messages are evicted or newer ones appended.
type Cursor string

// LoggedMessage is a message of a memory's log with its cursor.
type LoggedMessage struct {
	Message llm.Message
	Cursor  Cursor
}

// SnapshotStore is a Memory that stores snapshots. It is optional: the agent
// uses it when the memory implements it.
type SnapshotStore interface {
	Memory

	// LoadSnapshot returns the latest snapshot (nil when there is none) and
	// the log messages after it, with their cursors.
	LoadSnapshot(ctx context.Context) (*Snapshot, []LoggedMessage, error)

	// SaveSnapshot stores s as standing for every log message up to and
	// including upTo. prev is the snapshot the caller started from (nil for
	// none): when the latest snapshot is no longer prev, SaveSnapshot fails
	// with ErrSnapshotConflict, so two writers cannot overwrite each other's
	// cutoff. upTo must move past prev's (ErrInvalidCursor otherwise).
	SaveSnapshot(ctx context.Context, s Snapshot, upTo Cursor, prev *Snapshot) error
}

// SummaryMessages returns the messages that stand for a snapshot in a
// conversation: the summary as a user message, then the pinned messages. A
// user message keeps the summary in its place in the conversation; providers
// such as Anthropic and Gemini would move a system message into the system
// prompt.
func SummaryMessages(s Snapshot) []llm.Message {
	out := make([]llm.Message, 0, 1+len(s.Pinned))
	out = append(out, llm.UserMessage(llm.Text(SummarySystemMessagePrefix+s.Summary)))

	return append(out, s.Pinned...)
}
