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
