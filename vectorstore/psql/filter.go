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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/henomis/phero/vectorstore"
)

// filterSQL translates a portable vectorstore.Filter into SQL predicates over
// the jsonb payload column.
//
// It returns a string of " AND <predicate>" clauses to append to the base
// WHERE clause, plus the bind arguments those predicates reference. Argument
// placeholders start at $4. Payload keys and values are passed exclusively as
// bind parameters, never interpolated into the SQL text.
//
// A nil or empty filter yields an empty clause string.
//
//nolint:gocognit
func filterSQL(f *vectorstore.Filter) (string, []any, error) {
	if f == nil || len(f.Conditions) == 0 {
		return "", nil, nil
	}

	if err := f.Validate(); err != nil {
		return "", nil, err
	}

	var sb strings.Builder

	args := make([]any, 0, len(f.Conditions))
	next := 4

	placeholder := func(v any) string {
		args = append(args, v)
		p := fmt.Sprintf("$%d", next)
		next++

		return p
	}

	for _, c := range f.Conditions {
		switch c.Op {
		case vectorstore.OpEq:
			doc, err := containmentDoc(c.Key, c.Value)
			if err != nil {
				return "", nil, err
			}

			fmt.Fprintf(&sb, " AND payload @> %s::jsonb", placeholder(doc))
		case vectorstore.OpNe:
			doc, err := containmentDoc(c.Key, c.Value)
			if err != nil {
				return "", nil, err
			}
			// Require the key to exist so OpNe never matches payloads that
			// simply lack the key (see vectorstore.MatchPayload semantics).
			fmt.Fprintf(&sb, " AND (jsonb_exists(payload, %s) AND NOT payload @> %s::jsonb)",
				placeholder(c.Key), placeholder(doc))
		case vectorstore.OpIn:
			ors := make([]string, 0, len(c.Values))
			for _, v := range c.Values {
				doc, err := containmentDoc(c.Key, v)
				if err != nil {
					return "", nil, err
				}

				ors = append(ors, fmt.Sprintf("payload @> %s::jsonb", placeholder(doc)))
			}

			fmt.Fprintf(&sb, " AND (%s)", strings.Join(ors, " OR "))
		case vectorstore.OpGt, vectorstore.OpGte, vectorstore.OpLt, vectorstore.OpLte:
			value, ok := vectorstore.ToFloat64(c.Value)
			if !ok {
				return "", nil, vectorstore.ErrInvalidFilter
			}
			// Guard with jsonb_typeof so non-numeric payload values fail the
			// predicate instead of aborting the query on a cast error.
			fmt.Fprintf(&sb, " AND (jsonb_typeof(payload -> %s) = 'number' AND (payload ->> %s)::numeric %s %s)",
				placeholder(c.Key), placeholder(c.Key), sqlComparison(c.Op), placeholder(value))
		default:
			return "", nil, vectorstore.ErrUnsupportedFilterOp
		}
	}

	return sb.String(), args, nil
}

// containmentDoc builds the JSON document {key: value} used with the jsonb
// containment operator @>.
func containmentDoc(key string, value any) (string, error) {
	doc, err := json.Marshal(map[string]any{key: value})
	if err != nil {
		return "", fmt.Errorf("%w: %v", vectorstore.ErrInvalidFilter, err)
	}

	return string(doc), nil
}

func sqlComparison(op vectorstore.Op) string {
	switch op { //nolint:exhaustive // callers only pass comparison operators; others are unreachable
	case vectorstore.OpGt:
		return ">"
	case vectorstore.OpGte:
		return ">="
	case vectorstore.OpLt:
		return "<"
	default:
		return "<="
	}
}
