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
	"fmt"
	"strings"
	"sync"

	"github.com/henomis/phero/internal/sqlutil"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/memory"
)

var _ memory.Memory = (*Memory)(nil)

const (
	defaultTableName          = "conversation_memory"
	defaultSummarizeThreshold = 50
)

// Option configures a Memory instance.
type Option func(*Memory)

// Memory stores llm.Message values in PostgreSQL, scoped to a single session.
//
// The provided *sql.DB is treated as an injected dependency and is not owned by
// Memory (i.e. Memory does not Close it).
type Memory struct {
	db        *sql.DB
	sessionID string

	tableName    string
	ensureSchema bool

	// schemaMu guards schemaDone so that a transient failure during EnsureSchema
	// does not permanently prevent future attempts (unlike sync.Once).
	schemaMu   sync.Mutex
	schemaDone bool

	llm              llm.LLM
	summaryThreshold uint
	summarySize      uint
}

// New creates a new PostgreSQL-backed memory bound to sessionID.
func New(db *sql.DB, sessionID string, options ...Option) (*Memory, error) {
	if db == nil {
		return nil, ErrNilDB
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, ErrEmptySessionID
	}

	m := &Memory{
		db:           db,
		sessionID:    sessionID,
		tableName:    defaultTableName,
		ensureSchema: true,
	}

	for _, opt := range options {
		if opt != nil {
			opt(m)
		}
	}

	if strings.TrimSpace(m.tableName) == "" {
		return nil, ErrEmptyTableName
	}
	if _, err := sqlutil.QuoteQualifiedIdent(m.tableName); err != nil {
		return nil, ErrInvalidTableName
	}

	return m, nil
}

// WithTable overrides the SQL table used by the memory.
//
// Default is "conversation_memory".
//
// table must be a safe identifier in the form `table` or `schema.table`.
func WithTable(table string) Option {
	return func(m *Memory) {
		m.tableName = table
	}
}

// WithEnsureSchema controls whether the memory auto-creates its table/index.
//
// Default is true.
func WithEnsureSchema(ensure bool) Option {
	return func(m *Memory) {
		m.ensureSchema = ensure
	}
}

// WithSummarization enables automatic summarization when the number of stored
// messages exceeds summarizeThreshold.
//
// This mirrors the behavior of memory/simple.WithSummarization.
func WithSummarization(summaryLLM llm.LLM, summarizeThreshold, summarySize uint) Option {
	return func(m *Memory) {
		m.llm = summaryLLM

		if summarizeThreshold == 0 {
			summarizeThreshold = defaultSummarizeThreshold
		}

		m.summarySize = memory.ClampSummarySize(summarizeThreshold, summarySize)
		m.summaryThreshold = summarizeThreshold
	}
}

func (m *Memory) needSummarization() bool {
	return m.llm != nil && m.summaryThreshold > 0
}

// EnsureSchema creates the backing table and index if they do not exist.
//
// This is called automatically by Save/Retrieve/Clear when WithEnsureSchema(true)
// is enabled. Unlike a sync.Once-based guard, a failed attempt can be retried on
// the next call, which allows recovery from transient database outages.
func (m *Memory) EnsureSchema(ctx context.Context) error {
	m.schemaMu.Lock()
	defer m.schemaMu.Unlock()

	if m.schemaDone || !m.ensureSchema {
		return nil
	}

	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	if _, err := m.db.ExecContext(ctx, fmt.Sprintf(createTableSQLTemplate, table)); err != nil {
		return err
	}
	// index name uses the table name string; quoted identifiers aren't accepted
	// for index names. Use a deterministic safe name.
	idxName := sqlutil.SafeIndexName(m.tableName) + "_session_seq_idx"
	if _, err := m.db.ExecContext(ctx, fmt.Sprintf(createIndexSQLTemplate, idxName, table)); err != nil {
		return err
	}

	m.schemaDone = true
	return nil
}

// Save appends messages to the session history.
func (m *Memory) Save(ctx context.Context, messages []llm.Message) error {
	if err := m.EnsureSchema(ctx); err != nil {
		return err
	}
	if len(messages) == 0 {
		return nil
	}

	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	insertStmt := fmt.Sprintf(insertMessageSQLTemplate, table)

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, msg := range messages {
		b, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, insertStmt, m.sessionID, string(b)); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Summarization is done outside of the write transaction to avoid holding a
	// DB connection while calling the LLM.
	return m.maybeSummarize(ctx)
}

// Retrieve returns all messages currently in memory, ordered from oldest to newest.
//
// query is currently ignored (matching the behavior of memory/simple).
func (m *Memory) Retrieve(ctx context.Context, _ string) ([]llm.Message, error) {
	if err := m.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return nil, ErrInvalidTableName
	}

	stmt := fmt.Sprintf(selectAllMessagesSQLTemplate, table)
	rows, err := m.db.QueryContext(ctx, stmt, m.sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]llm.Message, 0)
	for rows.Next() {
		var msgBytes []byte
		if err := rows.Scan(&msgBytes); err != nil {
			return nil, err
		}
		var msg llm.Message
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Clear removes all messages for this session.
func (m *Memory) Clear(ctx context.Context) error {
	if err := m.EnsureSchema(ctx); err != nil {
		return err
	}

	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	stmt := fmt.Sprintf(clearSessionSQLTemplate, table)
	_, err = m.db.ExecContext(ctx, stmt, m.sessionID)
	return err
}

func (m *Memory) count(ctx context.Context) (int, error) {
	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return 0, ErrInvalidTableName
	}
	stmt := fmt.Sprintf(countMessagesSQLTemplate, table)
	var n int
	if err := m.db.QueryRowContext(ctx, stmt, m.sessionID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (m *Memory) maybeSummarize(ctx context.Context) error {
	if !m.needSummarization() {
		return nil
	}

	n, err := m.count(ctx)
	if err != nil {
		return err
	}
	if n < int(m.summaryThreshold) {
		return nil
	}

	msgs, err := m.Retrieve(ctx, "")
	if err != nil {
		return err
	}
	if len(msgs) < int(m.summaryThreshold) {
		return nil
	}
	if m.summarySize == 0 {
		return nil
	}
	if len(msgs) <= int(m.summarySize) {
		return nil
	}

	toSummarize := msgs[:m.summarySize]
	toAppend := msgs[m.summarySize:]

	history := memory.FormatSummaryPrompt(toSummarize)

	summaryMsg, err := m.llm.Execute(ctx, []llm.Message{history}, nil)
	if err != nil {
		return err
	}

	messagesToStore := []llm.Message{{
		Role:    llm.ChatMessageRoleSystem,
		Content: memory.SummarySystemMessagePrefix + summaryMsg.Message.Content,
	}}
	messagesToStore = append(messagesToStore, toAppend...)

	table, err := sqlutil.QuoteQualifiedIdent(m.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	clearStmt := fmt.Sprintf(clearSessionSQLTemplate, table)
	if _, err := tx.ExecContext(ctx, clearStmt, m.sessionID); err != nil {
		return err
	}

	insertStmt := fmt.Sprintf(insertMessageSQLTemplate, table)
	for _, msg := range messagesToStore {
		b, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, insertStmt, m.sessionID, string(b)); err != nil {
			return err
		}
	}

	return tx.Commit()
}
