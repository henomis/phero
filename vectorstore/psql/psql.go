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
	"math"
	"strconv"
	"strings"

	sqlutil "github.com/henomis/phero/internal/sql"
	"github.com/henomis/phero/vectorstore"
)

var _ vectorstore.Store = (*Store)(nil)

// Distance controls which pgvector operator is used for nearest-neighbor search.
//
// Score returned by Query is always "higher is more similar":
//   - Cosine: score = cosine similarity
//   - Euclidean: score = -L2 distance
//   - Dot: score = dot product
//
// This mirrors the high-level behavior of other vectorstore implementations
// where higher Score means closer/more relevant.
type Distance int

// Supported distance metrics.
const (
	DistanceCosine Distance = iota
	DistanceEuclid
	DistanceDot
)

const (
	defaultTableName = "vector_store"
	// firstIDArg is the placeholder index for the first element ID in DELETE
	// queries; $1 is reserved for the collection name.
	firstIDArg     = 2
	avgFloatChars  = 10
)

// Store is a PostgreSQL-backed implementation of vectorstore.Store.
//
// A Store is bound to a single logical collection.
// Points for all collections are stored in a single SQL table (by default
// "vector_store") and separated by a `collection` column.
//
// The provided *sql.DB is treated as an injected dependency and is not owned by
// the Store (i.e. Store does not Close it).
type Store struct {
	db              *sql.DB
	collection      string
	vectorSize      uint64
	tableName       string
	distance        Distance
	ensureExtension bool
}

// Option configures a Store created by New.
type Option func(*Store)

// WithVectorSize sets the vector size used when creating the backing table and
// validating points/queries.
//
// This is required.
func WithVectorSize(vectorSize uint64) Option {
	return func(s *Store) {
		s.vectorSize = vectorSize
	}
}

// WithDistance configures the distance operator used for Query.
//
// Default is DistanceCosine.
func WithDistance(distance Distance) Option {
	return func(s *Store) {
		s.distance = distance
	}
}

// WithEnsureExtension controls whether EnsureCollection tries to enable pgvector
// via `CREATE EXTENSION IF NOT EXISTS vector`.
//
// Default is true.
func WithEnsureExtension(ensure bool) Option {
	return func(s *Store) {
		s.ensureExtension = ensure
	}
}

// WithTable overrides the SQL table used by the store.
//
// Default is "vector_store".
//
// table must be a safe identifier in the form `table` or `schema.table`.
func WithTable(table string) Option {
	return func(s *Store) {
		s.tableName = table
	}
}

// New constructs a PostgreSQL-backed vectorstore bound to a single collection.
//
// Points for all collections are stored in a single SQL table (by default
// "vector_store") and separated by a `collection` column.
func New(db *sql.DB, collection string, opts ...Option) (*Store, error) {
	if db == nil {
		return nil, ErrNilDB
	}

	if strings.TrimSpace(collection) == "" {
		return nil, ErrEmptyCollection
	}

	s := &Store{
		db:              db,
		collection:      collection,
		tableName:       defaultTableName,
		distance:        DistanceCosine,
		ensureExtension: true,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if s.vectorSize == 0 {
		return nil, ErrInvalidVectorSize
	}

	if strings.TrimSpace(s.tableName) == "" {
		return nil, ErrEmptyTableName
	}

	if _, err := sqlutil.QuoteQualifiedIdent(s.tableName); err != nil {
		return nil, ErrInvalidTableName
	}

	return s, nil
}

// EnsureCollection ensures that the backing table exists.
//
// If pgvector is not enabled in the database, it will (by default) attempt to
// enable it via CREATE EXTENSION.
func (s *Store) EnsureCollection(ctx context.Context) error {
	if s.ensureExtension {
		if _, err := s.db.ExecContext(ctx, createPgvectorExtensionSQL); err != nil {
			return err
		}
	}

	table, err := sqlutil.QuoteQualifiedIdent(s.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	stmt := fmt.Sprintf(createTableSQLTemplate, table, s.vectorSize)

	_, err = s.db.ExecContext(ctx, stmt)

	return err
}

// Upsert inserts or updates points in the configured table.
func (s *Store) Upsert(ctx context.Context, points []vectorstore.Point) error {
	if len(points) == 0 {
		return vectorstore.ErrEmptyPoints
	}

	table, err := sqlutil.QuoteQualifiedIdent(s.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	stmt := fmt.Sprintf(upsertSQLTemplate, table)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, p := range points {
		if p.ID == "" {
			return ErrPointIDRequired
		}

		if len(p.Vector) == 0 {
			return &EmptyVectorError{PointID: p.ID}
		}

		if uint64(len(p.Vector)) != s.vectorSize {
			return &VectorSizeMismatchError{Expected: s.vectorSize, Got: len(p.Vector)}
		}

		vecLit, litErr := vectorLiteral(p.Vector)
		if litErr != nil {
			return litErr
		}

		var payload any

		if p.Payload != nil {
			b, marshalErr := json.Marshal(p.Payload)
			if marshalErr != nil {
				return marshalErr
			}

			payload = string(b)
		} else {
			payload = nil
		}

		if _, execErr := tx.ExecContext(ctx, stmt, s.collection, p.ID, vecLit, payload); execErr != nil {
			return execErr
		}
	}

	return tx.Commit()
}

// Query returns the top-k nearest points to the query vector.
//
// A vectorstore.Filter passed via vectorstore.WithFilter is translated to
// parameterized JSONB predicates over the payload column and evaluated by
// PostgreSQL.
func (s *Store) Query(
	ctx context.Context, query vectorstore.Vector, limit uint64, opts ...vectorstore.QueryOption,
) ([]vectorstore.ScoredPoint, error) {
	if len(query) == 0 {
		return nil, vectorstore.ErrEmptyQuery
	}

	if limit == 0 {
		return []vectorstore.ScoredPoint{}, nil
	}

	if uint64(len(query)) != s.vectorSize {
		return nil, &VectorSizeMismatchError{Expected: s.vectorSize, Got: len(query)}
	}

	table, err := sqlutil.QuoteQualifiedIdent(s.tableName)
	if err != nil {
		return nil, ErrInvalidTableName
	}

	vecLit, err := vectorLiteral(query)
	if err != nil {
		return nil, err
	}

	op, scoreExpr, err := distanceSQL(s.distance)
	if err != nil {
		return nil, err
	}

	cfg := vectorstore.ApplyQueryOptions(opts)

	filterClause, filterArgs, err := filterSQL(cfg.Filter)
	if err != nil {
		return nil, err
	}

	stmt := fmt.Sprintf(querySQLTemplate, scoreExpr, table, filterClause, op)

	args := append([]any{vecLit, s.collection, limit}, filterArgs...)

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = rows.Close()
	}()

	out := make([]vectorstore.ScoredPoint, 0)

	for rows.Next() {
		var (
			id           string
			score64      float64
			payloadBytes []byte
		)
		if scanErr := rows.Scan(&id, &score64, &payloadBytes); scanErr != nil {
			return nil, scanErr
		}

		payload := map[string]any{}
		if len(payloadBytes) > 0 {
			if unmarshalErr := json.Unmarshal(payloadBytes, &payload); unmarshalErr != nil {
				return nil, &PayloadDecodeError{PointID: id, Cause: unmarshalErr}
			}
		}

		out = append(out, vectorstore.ScoredPoint{ID: id, Score: float32(score64), Payload: payload})
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return out, nil
}

// Delete deletes points by ID.
func (s *Store) Delete(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return vectorstore.ErrEmptyIDs
	}

	filtered := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			filtered = append(filtered, id)
		}
	}

	if len(filtered) == 0 {
		return vectorstore.ErrEmptyIDs
	}

	table, err := sqlutil.QuoteQualifiedIdent(s.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	placeholders := make([]string, 0, len(filtered))
	args := make([]any, 0, len(filtered)+1)

	args = append(args, s.collection)
	for i, id := range filtered {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+firstIDArg))
		args = append(args, id)
	}

	stmt := fmt.Sprintf(deleteByIDsSQLTemplate, table, strings.Join(placeholders, ","))
	_, err = s.db.ExecContext(ctx, stmt, args...)

	return err
}

// Count returns the number of points in the configured collection.
func (s *Store) Count(ctx context.Context) (uint64, error) {
	table, err := sqlutil.QuoteQualifiedIdent(s.tableName)
	if err != nil {
		return 0, ErrInvalidTableName
	}

	var count uint64

	err = s.db.QueryRowContext(ctx, fmt.Sprintf(countSQLTemplate, table), s.collection).Scan(&count)

	return count, err
}

// Clear deletes all points in the configured table.
func (s *Store) Clear(ctx context.Context) error {
	table, err := sqlutil.QuoteQualifiedIdent(s.tableName)
	if err != nil {
		return ErrInvalidTableName
	}

	stmt := fmt.Sprintf(clearSQLTemplate, table)
	_, err = s.db.ExecContext(ctx, stmt, s.collection)

	return err
}

func distanceSQL(d Distance) (op, scoreExpr string, err error) {
	switch d {
	case DistanceCosine:
		return "<=>", "(1 - (embedding <=> $1::vector))", nil
	case DistanceEuclid:
		return "<->", "-(embedding <-> $1::vector)", nil
	case DistanceDot:
		return "<#>", "-(embedding <#> $1::vector)", nil
	default:
		return "", "", fmt.Errorf("unknown distance: %d", d)
	}
}

func vectorLiteral(vec []float32) (string, error) {
	if len(vec) == 0 {
		return "[]", nil
	}

	b := strings.Builder{}
	b.Grow(len(vec) * avgFloatChars)
	b.WriteByte('[')

	for i, v := range vec {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return "", &InvalidVectorValueError{Index: i, Value: v}
		}

		if i > 0 {
			b.WriteByte(',')
		}

		b.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}

	b.WriteByte(']')

	return b.String(), nil
}
