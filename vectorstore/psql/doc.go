// Package psql implements the vectorstore.Store interface using PostgreSQL + pgvector.
//
// The Store persists vectors in a single SQL table and uses pgvector distance
// operators for similarity search.
//
// Requirements:
//   - PostgreSQL with the pgvector extension installed.
//   - A database/sql driver for PostgreSQL (for example pgx stdlib).
//
// Basic usage:
//
//	import (
//		"context"
//		"database/sql"
//		"os"
//
//		_ "github.com/jackc/pgx/v5/stdlib"
//
//		"github.com/henomis/phero/vectorstore"
//		vspql "github.com/henomis/phero/vectorstore/psql"
//	)
//
//	db, _ := sql.Open("pgx", os.Getenv("DATABASE_URL"))
//	store, _ := vspql.New(db, "my_collection", vspql.WithVectorSize(1536))
//	_ = store.EnsureCollection(context.Background())
//	_ = store.Upsert(context.Background(), []vectorstore.Point{{ID: "1", Vector: make([]float32, 1536)}})
//	res, _ := store.Query(context.Background(), make([]float32, 1536), 5)
//
// Notes:
//   - EnsureCollection attempts to run `CREATE EXTENSION IF NOT EXISTS vector`.
//     If your DB user lacks privileges, either grant them or disable this via
//     WithEnsureExtension(false) after you install/enable the extension.
//   - By default, all points are stored in a single SQL table named
//     "vector_store" with a `collection` column. Use WithTable("...") to
//     override the table name.
package psql
