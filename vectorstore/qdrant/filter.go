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

package qdrant

import (
	qdrantapi "github.com/qdrant/go-client/qdrant"

	"github.com/henomis/phero/vectorstore"
)

// translateFilter converts a portable vectorstore.Filter into a native Qdrant
// filter evaluated server-side. A nil filter yields a nil Qdrant filter.
//
//nolint:gocognit
func translateFilter(f *vectorstore.Filter) (*qdrantapi.Filter, error) {
	if f == nil || len(f.Conditions) == 0 {
		return nil, nil
	}

	if err := f.Validate(); err != nil {
		return nil, err
	}

	out := &qdrantapi.Filter{}

	for _, c := range f.Conditions {
		switch c.Op {
		case vectorstore.OpEq:
			cond, err := eqCondition(c.Key, c.Value)
			if err != nil {
				return nil, err
			}

			out.Must = append(out.Must, cond)
		case vectorstore.OpNe:
			cond, err := eqCondition(c.Key, c.Value)
			if err != nil {
				return nil, err
			}
			// Exclude both points matching the value and points missing the
			// key, so OpNe only matches points that contain the key with a
			// different value (see vectorstore.MatchPayload semantics).
			out.MustNot = append(out.MustNot, cond, qdrantapi.NewIsEmpty(c.Key))
		case vectorstore.OpIn:
			conds := make([]*qdrantapi.Condition, 0, len(c.Values))
			for _, v := range c.Values {
				cond, err := eqCondition(c.Key, v)
				if err != nil {
					return nil, err
				}

				conds = append(conds, cond)
			}

			out.Must = append(out.Must, qdrantapi.NewFilterAsCondition(&qdrantapi.Filter{Should: conds}))
		case vectorstore.OpGt, vectorstore.OpGte, vectorstore.OpLt, vectorstore.OpLte:
			value, ok := vectorstore.ToFloat64(c.Value)
			if !ok {
				return nil, vectorstore.ErrInvalidFilter
			}

			r := &qdrantapi.Range{}

			switch c.Op { //nolint:exhaustive // outer case restricts to OpGt/Gte/Lt/Lte; others are unreachable
			case vectorstore.OpGt:
				r.Gt = &value
			case vectorstore.OpGte:
				r.Gte = &value
			case vectorstore.OpLt:
				r.Lt = &value
			default:
				r.Lte = &value
			}

			out.Must = append(out.Must, qdrantapi.NewRange(c.Key, r))
		default:
			return nil, vectorstore.ErrUnsupportedFilterOp
		}
	}

	return out, nil
}

// eqCondition builds a Qdrant equality condition for a single value.
//
// Strings and booleans map to native match conditions. Integers map to
// integer matches; floating-point values use a closed range [v, v] because
// Qdrant has no double match condition.
func eqCondition(key string, value any) (*qdrantapi.Condition, error) {
	switch v := value.(type) {
	case string:
		return qdrantapi.NewMatchKeyword(key, v), nil
	case bool:
		return qdrantapi.NewMatchBool(key, v), nil
	case float32, float64:
		f, _ := vectorstore.ToFloat64(v)
		return qdrantapi.NewRange(key, &qdrantapi.Range{Gte: &f, Lte: &f}), nil
	default:
		f, ok := vectorstore.ToFloat64(v)
		if !ok {
			return nil, vectorstore.ErrInvalidFilter
		}

		return qdrantapi.NewMatchInt(key, int64(f)), nil
	}
}
