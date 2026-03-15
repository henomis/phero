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
