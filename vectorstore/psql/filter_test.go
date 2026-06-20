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
	"errors"
	"reflect"
	"testing"

	"github.com/henomis/phero/vectorstore"
)

func TestFilterSQLNil(t *testing.T) {
	clause, args, err := filterSQL(nil)
	if err != nil {
		t.Fatalf("filterSQL(nil) error = %v", err)
	}

	if clause != "" || len(args) != 0 {
		t.Fatalf("filterSQL(nil) = %q, %v; want empty", clause, args)
	}
}

func TestFilterSQLEq(t *testing.T) {
	clause, args, err := filterSQL(vectorstore.NewFilter(vectorstore.Eq("category", "news")))
	if err != nil {
		t.Fatalf("filterSQL() error = %v", err)
	}

	wantClause := " AND payload @> $4::jsonb"
	if clause != wantClause {
		t.Fatalf("clause = %q, want %q", clause, wantClause)
	}

	wantArgs := []any{`{"category":"news"}`}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
}

func TestFilterSQLNe(t *testing.T) {
	clause, args, err := filterSQL(vectorstore.NewFilter(vectorstore.Ne("category", "news")))
	if err != nil {
		t.Fatalf("filterSQL() error = %v", err)
	}

	wantClause := " AND (jsonb_exists(payload, $4) AND NOT payload @> $5::jsonb)"
	if clause != wantClause {
		t.Fatalf("clause = %q, want %q", clause, wantClause)
	}

	wantArgs := []any{"category", `{"category":"news"}`}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
}

func TestFilterSQLIn(t *testing.T) {
	clause, args, err := filterSQL(vectorstore.NewFilter(vectorstore.In("year", 2020, 2024)))
	if err != nil {
		t.Fatalf("filterSQL() error = %v", err)
	}

	wantClause := " AND (payload @> $4::jsonb OR payload @> $5::jsonb)"
	if clause != wantClause {
		t.Fatalf("clause = %q, want %q", clause, wantClause)
	}

	wantArgs := []any{`{"year":2020}`, `{"year":2024}`}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
}

func TestFilterSQLRange(t *testing.T) {
	clause, args, err := filterSQL(vectorstore.NewFilter(vectorstore.Gte("year", 2021)))
	if err != nil {
		t.Fatalf("filterSQL() error = %v", err)
	}

	wantClause := " AND (jsonb_typeof(payload -> $4) = 'number' AND (payload ->> $5)::numeric >= $6)"
	if clause != wantClause {
		t.Fatalf("clause = %q, want %q", clause, wantClause)
	}

	wantArgs := []any{"year", "year", float64(2021)}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("args = %v, want %v", args, wantArgs)
	}
}

func TestFilterSQLMultipleConditions(t *testing.T) {
	f := vectorstore.NewFilter(
		vectorstore.Eq("category", "news"),
		vectorstore.Lt("year", 2030),
	)

	clause, args, err := filterSQL(f)
	if err != nil {
		t.Fatalf("filterSQL() error = %v", err)
	}

	wantClause := " AND payload @> $4::jsonb" +
		" AND (jsonb_typeof(payload -> $5) = 'number' AND (payload ->> $6)::numeric < $7)"
	if clause != wantClause {
		t.Fatalf("clause = %q, want %q", clause, wantClause)
	}

	if len(args) != 4 {
		t.Fatalf("len(args) = %d, want 4", len(args))
	}
}

func TestFilterSQLInvalid(t *testing.T) {
	_, _, err := filterSQL(vectorstore.NewFilter(vectorstore.Eq("", "x")))
	if !errors.Is(err, vectorstore.ErrInvalidFilter) {
		t.Fatalf("filterSQL() error = %v, want ErrInvalidFilter", err)
	}
}
