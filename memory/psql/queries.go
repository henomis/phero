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

package psql

// Centralized SQL used by the PostgreSQL memory implementation.
//
// These are templates; callers should format with fmt.Sprintf where needed.

// createTableSQLTemplate expects:
//   - %s: quoted table name
const createTableSQLTemplate = `
CREATE TABLE IF NOT EXISTS %s (
	session_id text        NOT NULL,
	seq        bigserial   NOT NULL,
	message    jsonb       NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (session_id, seq)
)
`

// createIndexSQLTemplate expects:
//   - %s: safe index name (unquoted)
//   - %s: quoted table name
const createIndexSQLTemplate = `
CREATE INDEX IF NOT EXISTS %s
ON %s (session_id, seq)
`

// insertMessageSQLTemplate expects:
//   - %s: quoted table name
const insertMessageSQLTemplate = `
INSERT INTO %s (session_id, message)
VALUES ($1, $2::jsonb)
`

// selectAllMessagesSQLTemplate expects:
//   - %s: quoted table name
const selectAllMessagesSQLTemplate = `
SELECT message
FROM %s
WHERE session_id = $1
ORDER BY seq ASC
`

// countMessagesSQLTemplate expects:
//   - %s: quoted table name
const countMessagesSQLTemplate = `
SELECT COUNT(*)
FROM %s
WHERE session_id = $1
`

// clearSessionSQLTemplate expects:
//   - %s: quoted table name
const clearSessionSQLTemplate = `
DELETE FROM %s
WHERE session_id = $1
`

// createSnapshotTableSQLTemplate expects:
//   - %s: quoted snapshot table name
//
// One row per session: its latest snapshot, standing for the session's
// messages up to and including upto_seq.
const createSnapshotTableSQLTemplate = `
CREATE TABLE IF NOT EXISTS %s (
	session_id text        PRIMARY KEY,
	upto_seq   bigint      NOT NULL,
	snapshot   jsonb       NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
)
`

// selectSnapshotSQLTemplate expects:
//   - %s: quoted snapshot table name
const selectSnapshotSQLTemplate = `
SELECT upto_seq, snapshot
FROM %s
WHERE session_id = $1
`

// selectMessagesAfterSQLTemplate expects:
//   - %s: quoted table name
const selectMessagesAfterSQLTemplate = `
SELECT seq, message
FROM %s
WHERE session_id = $1 AND seq > $2
ORDER BY seq ASC
`

// messageExistsSQLTemplate expects:
//   - %s: quoted table name
const messageExistsSQLTemplate = `
SELECT EXISTS (SELECT 1 FROM %s WHERE session_id = $1 AND seq = $2)
`

// insertSnapshotSQLTemplate expects:
//   - %s: quoted snapshot table name
//
// It inserts the session's first snapshot; a row already there (another
// writer's) is left alone, and no row is affected.
const insertSnapshotSQLTemplate = `
INSERT INTO %s (session_id, upto_seq, snapshot)
VALUES ($1, $2, $3::jsonb)
ON CONFLICT (session_id) DO NOTHING
`

// updateSnapshotSQLTemplate expects:
//   - %s: quoted snapshot table name
//
// It replaces the snapshot only if it is still the one saved up to $4: a
// compare-and-swap, affecting no row when another writer moved it.
const updateSnapshotSQLTemplate = `
UPDATE %s
SET upto_seq = $2, snapshot = $3::jsonb, created_at = now()
WHERE session_id = $1 AND upto_seq = $4
`

// pruneMessagesSQLTemplate expects:
//   - %s: quoted table name
const pruneMessagesSQLTemplate = `
DELETE FROM %s
WHERE session_id = $1 AND seq <= $2
`
