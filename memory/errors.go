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

import "errors"

// ErrSnapshotsUnsupported is returned by a SnapshotStore's snapshot methods
// when the memory is set up in a way snapshots cannot work with, such as a
// backend's own summarization, which rewrites the log.
var ErrSnapshotsUnsupported = errors.New("memory: snapshots unsupported with this configuration")

// ErrSnapshotConflict is returned by SaveSnapshot when the latest snapshot is
// no longer the one the caller started from: another writer saved one first.
var ErrSnapshotConflict = errors.New("memory: snapshot conflict")

// ErrInvalidCursor is returned by SaveSnapshot for a cursor that is not one
// of the memory's, names no stored message, or does not move past the
// previous snapshot.
var ErrInvalidCursor = errors.New("memory: invalid cursor")
