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

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	sqlutil "github.com/henomis/phero/v2/internal/sql"
	"github.com/henomis/phero/v2/llm"
	"github.com/henomis/phero/v2/memory"
)

// snapshotTableSuffix names the snapshot table after the message table.
const snapshotTableSuffix = "_snapshots"

// WithPruneCompacted deletes the messages a snapshot stands for when it is
// saved, in the same transaction. Off by default: the table keeps the whole
// log.
func WithPruneCompacted(prune bool) Option {
	return func(m *Memory) {
		m.prune = prune
	}
}

// snapshotTable returns the quoted name of the snapshot table: the message
// table's name with "_snapshots" appended.
func (m *Memory) snapshotTable() (string, error) {
	table, err := sqlutil.QuoteQualifiedIdent(m.tableName + snapshotTableSuffix)
	if err != nil {
		return "", ErrInvalidTableName
	}

	return table, nil
}

// LoadSnapshot returns the session's latest snapshot (nil when there is none)
// and the messages after it, with their cursors (the rows' seq), read in one
// repeatable-read transaction. It returns memory.ErrSnapshotsUnsupported with
// WithSummarization, which rewrites the session's rows.
func (m *Memory) LoadSnapshot(ctx context.Context) (*memory.Snapshot, []memory.LoggedMessage, error) {
	if m.llm != nil {
		return nil, nil, memory.ErrSnapshotsUnsupported
	}

	if err := m.EnsureSchema(ctx); err != nil {
		return nil, nil, err
	}

	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return nil, nil, ErrInvalidTableName
	}

	tx, err := m.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	snap, upTo, err := m.currentSnapshot(ctx, tx)
	if err != nil {
		return nil, nil, err
	}

	logged, err := loggedAfter(ctx, tx, fmt.Sprintf(selectMessagesAfterSQLTemplate, table), m.sessionID, upTo)
	if err != nil {
		return nil, nil, err
	}

	return snap, logged, tx.Commit()
}

// SaveSnapshot stores s as standing for the session's messages up to and
// including upTo, a cursor LoadSnapshot returned, when the latest snapshot is
// still prev (memory.ErrSnapshotConflict otherwise, even against another
// process). upTo must name a message of the session after prev's
// (memory.ErrInvalidCursor otherwise). With WithPruneCompacted, the covered
// messages are deleted in the same transaction.
func (m *Memory) SaveSnapshot(ctx context.Context, s memory.Snapshot, upTo memory.Cursor, prev *memory.Snapshot) error {
	if m.llm != nil {
		return memory.ErrSnapshotsUnsupported
	}

	if err := m.EnsureSchema(ctx); err != nil {
		return err
	}

	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	snapshots, err := m.snapshotTable()
	if err != nil {
		return err
	}

	pos, prevPos, err := cutoff(upTo, prev)
	if err != nil {
		return err
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool

	existsStmt := fmt.Sprintf(messageExistsSQLTemplate, table)
	if err = tx.QueryRowContext(ctx, existsStmt, m.sessionID, pos).Scan(&exists); err != nil {
		return err
	}

	if !exists {
		return fmt.Errorf("%w: %q", memory.ErrInvalidCursor, upTo)
	}

	s.UpTo = upTo

	data, err := json.Marshal(s)
	if err != nil {
		return err
	}

	var res sql.Result

	if prev == nil {
		stmt := fmt.Sprintf(insertSnapshotSQLTemplate, snapshots)
		res, err = tx.ExecContext(ctx, stmt, m.sessionID, pos, string(data))
	} else {
		stmt := fmt.Sprintf(updateSnapshotSQLTemplate, snapshots)
		res, err = tx.ExecContext(ctx, stmt, m.sessionID, pos, string(data), prevPos)
	}

	if err != nil {
		return err
	}

	if n, rowsErr := res.RowsAffected(); rowsErr != nil || n != 1 {
		return errors.Join(memory.ErrSnapshotConflict, rowsErr)
	}

	if m.prune {
		if _, err = tx.ExecContext(ctx, fmt.Sprintf(pruneMessagesSQLTemplate, table), m.sessionID, pos); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// cutoff parses upTo and prev's cursor, and checks that upTo moves past it.
func cutoff(upTo memory.Cursor, prev *memory.Snapshot) (int64, int64, error) {
	pos, err := strconv.ParseInt(string(upTo), 10, 64)
	if err != nil || pos <= 0 {
		return 0, 0, fmt.Errorf("%w: %q", memory.ErrInvalidCursor, upTo)
	}

	if prev == nil {
		return pos, 0, nil
	}

	prevPos, err := strconv.ParseInt(string(prev.UpTo), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: previous %q", memory.ErrInvalidCursor, prev.UpTo)
	}

	if pos <= prevPos {
		return 0, 0, fmt.Errorf("%w: %q does not move past %q", memory.ErrInvalidCursor, upTo, prev.UpTo)
	}

	return pos, prevPos, nil
}

// currentSnapshot returns the session's snapshot and the seq it covers up to,
// 0 when there is none.
func (m *Memory) currentSnapshot(ctx context.Context, tx *sql.Tx) (*memory.Snapshot, int64, error) {
	snapshots, err := m.snapshotTable()
	if err != nil {
		return nil, 0, err
	}

	var (
		upTo int64
		data []byte
	)

	err = tx.QueryRowContext(ctx, fmt.Sprintf(selectSnapshotSQLTemplate, snapshots), m.sessionID).Scan(&upTo, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, nil
	}

	if err != nil {
		return nil, 0, err
	}

	var snap memory.Snapshot
	if err = json.Unmarshal(data, &snap); err != nil {
		return nil, 0, err
	}

	snap.UpTo = memory.Cursor(strconv.FormatInt(upTo, 10))

	return &snap, upTo, nil
}

// loggedAfter returns the session's messages after seq upTo, with their
// cursors, leaving out leading tool results whose call is covered.
func loggedAfter(ctx context.Context, tx *sql.Tx, stmt, sessionID string, upTo int64) ([]memory.LoggedMessage, error) {
	rows, err := tx.QueryContext(ctx, stmt, sessionID, upTo)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []memory.LoggedMessage

	for rows.Next() {
		var (
			seq  int64
			data []byte
		)

		if err = rows.Scan(&seq, &data); err != nil {
			return nil, err
		}

		var msg llm.Message
		if err = json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}

		if len(out) == 0 && msg.Role == llm.RoleTool {
			continue
		}

		out = append(out, memory.LoggedMessage{Message: msg, Cursor: memory.Cursor(strconv.FormatInt(seq, 10))})
	}

	return out, rows.Err()
}
