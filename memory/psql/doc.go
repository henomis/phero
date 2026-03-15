// Package psql implements the memory.Memory interface using PostgreSQL.
//
// This memory implementation is bound to a single session (sessionID) provided
// at construction time.
//
// Requirements:
//   - PostgreSQL.
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
//		"github.com/henomis/phero/memory/psql"
//	)
//
//	db, _ := sql.Open("pgx", os.Getenv("DATABASE_URL"))
//	mem, _ := psql.New(db, "session-123")
//	_ = mem.Save(context.Background(), messages)
package psql
