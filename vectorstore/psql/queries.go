package psql

// Centralized SQL used by the pgvector store.
//
// These are templates; callers should format with fmt.Sprintf where needed.

const createPgvectorExtensionSQL = `CREATE EXTENSION IF NOT EXISTS vector`

// createTableSQLTemplate expects:
//   - %s: quoted table name
//   - %d: vector size
const createTableSQLTemplate = `
CREATE TABLE IF NOT EXISTS %s (
	collection  text        NOT NULL,
	id          text        NOT NULL,
	embedding   vector(%d)  NOT NULL,
	payload     jsonb,
	created_at  timestamptz NOT NULL DEFAULT now(),
	updated_at  timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (collection, id)
)
`

// upsertSQLTemplate expects:
//   - %s: quoted table name
const upsertSQLTemplate = `
INSERT INTO %s (collection, id, embedding, payload, updated_at)
VALUES ($1, $2, $3::vector, $4::jsonb, now())
ON CONFLICT (collection, id)
DO UPDATE SET
	embedding = EXCLUDED.embedding,
	payload = EXCLUDED.payload,
	updated_at = now()
`

// querySQLTemplate expects:
//   - %s: score expression
//   - %s: quoted table name
//   - %s: pgvector operator
const querySQLTemplate = `
SELECT id,
       %s AS score,
       COALESCE(payload, '{}'::jsonb) AS payload
FROM %s
WHERE collection = $2
ORDER BY embedding %s $1::vector
LIMIT $3
`

// deleteByIDsSQLTemplate expects:
//   - %s: quoted table name
//   - %s: placeholders list (e.g. $2,$3,$4)
const deleteByIDsSQLTemplate = `DELETE FROM %s WHERE collection = $1 AND id IN (%s)`

// clearSQLTemplate expects:
//   - %s: quoted table name
const clearSQLTemplate = `DELETE FROM %s WHERE collection = $1`
