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

package jsonfile

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"time"

	"github.com/henomis/phero/v2/llm"
)

// SetMeta stores value under key in the file's metadata and writes the
// file; an empty value removes the key. Use it for what describes a session,
// such as its title or the model it ran with: Inspect reads it back without
// opening the memory. Clear removes the metadata too.
func (m *Memory) SetMeta(key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	old := m.meta
	m.meta = maps.Clone(old)

	if value == "" {
		delete(m.meta, key)
	} else {
		if m.meta == nil {
			m.meta = map[string]string{}
		}

		m.meta[key] = value
	}

	if err := m.persist(); err != nil {
		m.meta = old
		return err
	}

	return nil
}

// Meta returns a copy of the file's metadata; nil when there is none.
func (m *Memory) Meta() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return maps.Clone(m.meta)
}

// Info describes a memory file, as Inspect reads it.
type Info struct {
	// Messages is the number of messages ever saved to the file, those
	// WithPruneCompacted dropped included.
	Messages int
	// UpdatedAt is when the file was last written.
	UpdatedAt time.Time
	// FirstUserText is the text of the first user message the file still
	// holds; empty when there is none, as after WithPruneCompacted dropped
	// it. Store what must outlive pruning with SetMeta.
	FirstUserText string
	// Compacted reports whether the file holds a snapshot, so Retrieve
	// returns its summary in place of the messages it covers.
	Compacted bool
	// Meta is the metadata stored with SetMeta; nil when there is none.
	Meta map[string]string
}

// Inspect reads the memory file at path without opening it as a Memory:
// for listing sessions. The messages are not decoded, except up to the first
// user one. It returns an error matching os.ErrNotExist when there is no such
// file, and ErrUnknownFileVersion for a format this version does not know.
func Inspect(path string) (Info, error) {
	if path == "" {
		return Info{}, ErrEmptyFilePath
	}

	data, err := os.ReadFile(path) //nolint:gosec // reading the caller's memory file is the point
	if err != nil {
		return Info{}, err
	}

	stat, err := os.Stat(path)
	if err != nil {
		return Info{}, err
	}

	f, err := decodeFile[json.RawMessage](data)
	if err != nil {
		return Info{}, fmt.Errorf("%s: %w", path, err)
	}

	info := Info{
		Messages:  f.Offset + len(f.Messages),
		UpdatedAt: stat.ModTime(),
		Compacted: f.Snapshot != nil,
		Meta:      f.Meta,
	}

	for _, raw := range f.Messages {
		var role struct {
			Role string `json:"role"`
		}

		if err = json.Unmarshal(raw, &role); err != nil {
			return Info{}, fmt.Errorf("%s: %w", path, err)
		}

		if role.Role != llm.RoleUser {
			continue
		}

		var msg llm.Message
		if err = json.Unmarshal(raw, &msg); err != nil {
			return Info{}, fmt.Errorf("%s: %w", path, err)
		}

		info.FirstUserText = msg.TextContent()

		break
	}

	return info, nil
}
