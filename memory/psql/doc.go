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
