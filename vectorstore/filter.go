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

package vectorstore

// Op is a filter condition operator.
type Op string

// Supported filter operators.
const (
	// OpEq matches payloads whose value for Key equals Value.
	OpEq Op = "eq"
	// OpNe matches payloads that contain Key with a value different from Value.
	OpNe Op = "ne"
	// OpIn matches payloads whose value for Key equals any of Values.
	OpIn Op = "in"
	// OpGt matches payloads whose numeric value for Key is greater than Value.
	OpGt Op = "gt"
	// OpGte matches payloads whose numeric value for Key is greater than or equal to Value.
	OpGte Op = "gte"
	// OpLt matches payloads whose numeric value for Key is less than Value.
	OpLt Op = "lt"
	// OpLte matches payloads whose numeric value for Key is less than or equal to Value.
	OpLte Op = "lte"
)

// Condition is a single payload predicate.
//
// Value is used by all operators except OpIn, which uses Values.
// Supported value types are strings, booleans and numbers; the range
// operators (gt/gte/lt/lte) require numeric values.
type Condition struct {
	Key    string
	Op     Op
	Value  any
	Values []any
}

// Filter combines conditions with AND semantics: a payload matches the filter
// only when it satisfies every condition.
//
// A condition on a missing payload key never matches, including OpNe.
type Filter struct {
	Conditions []Condition
}

// NewFilter creates a Filter that ANDs the provided conditions.
func NewFilter(conditions ...Condition) *Filter {
	return &Filter{Conditions: conditions}
}

// Eq returns a Condition matching payloads whose value for key equals value.
func Eq(key string, value any) Condition {
	return Condition{Key: key, Op: OpEq, Value: value}
}

// Ne returns a Condition matching payloads that contain key with a value
// different from value.
func Ne(key string, value any) Condition {
	return Condition{Key: key, Op: OpNe, Value: value}
}

// In returns a Condition matching payloads whose value for key equals any of
// the provided values.
func In(key string, values ...any) Condition {
	return Condition{Key: key, Op: OpIn, Values: values}
}

// Gt returns a Condition matching payloads whose numeric value for key is
// greater than value.
func Gt(key string, value any) Condition {
	return Condition{Key: key, Op: OpGt, Value: value}
}

// Gte returns a Condition matching payloads whose numeric value for key is
// greater than or equal to value.
func Gte(key string, value any) Condition {
	return Condition{Key: key, Op: OpGte, Value: value}
}

// Lt returns a Condition matching payloads whose numeric value for key is
// less than value.
func Lt(key string, value any) Condition {
	return Condition{Key: key, Op: OpLt, Value: value}
}

// Lte returns a Condition matching payloads whose numeric value for key is
// less than or equal to value.
func Lte(key string, value any) Condition {
	return Condition{Key: key, Op: OpLte, Value: value}
}

// Validate checks that every condition has a non-empty key, a known operator,
// and operands compatible with that operator.
func (f *Filter) Validate() error {
	if f == nil {
		return nil
	}
	for _, c := range f.Conditions {
		if c.Key == "" {
			return ErrInvalidFilter
		}
		switch c.Op {
		case OpEq, OpNe:
			if c.Value == nil {
				return ErrInvalidFilter
			}
		case OpIn:
			if len(c.Values) == 0 {
				return ErrInvalidFilter
			}
		case OpGt, OpGte, OpLt, OpLte:
			if _, ok := ToFloat64(c.Value); !ok {
				return ErrInvalidFilter
			}
		default:
			return ErrUnsupportedFilterOp
		}
	}
	return nil
}

// QueryConfig collects per-query options applied by Store implementations.
type QueryConfig struct {
	Filter *Filter
}

// QueryOption configures a single Query call.
type QueryOption func(*QueryConfig)

// WithFilter restricts query results to points whose payload matches f.
func WithFilter(f *Filter) QueryOption {
	return func(c *QueryConfig) {
		c.Filter = f
	}
}

// ApplyQueryOptions folds opts into a QueryConfig. It is a helper for Store
// implementations.
func ApplyQueryOptions(opts []QueryOption) QueryConfig {
	var cfg QueryConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

// MatchPayload reports whether payload satisfies f. A nil filter (or one with
// no conditions) matches every payload.
//
// Numbers are compared after normalization to float64, so a payload value of
// int64(7) matches a condition value of float64(7). Conditions on keys absent
// from the payload never match, including OpNe.
//
// MatchPayload is the reference filter semantics; Store implementations that
// cannot push a filter down to the backend use it for client-side filtering.
func MatchPayload(f *Filter, payload map[string]any) bool {
	if f == nil {
		return true
	}
	for _, c := range f.Conditions {
		if !matchCondition(c, payload) {
			return false
		}
	}
	return true
}

func matchCondition(c Condition, payload map[string]any) bool {
	value, ok := payload[c.Key]
	if !ok {
		return false
	}

	switch c.Op {
	case OpEq:
		return valuesEqual(value, c.Value)
	case OpNe:
		return !valuesEqual(value, c.Value)
	case OpIn:
		for _, candidate := range c.Values {
			if valuesEqual(value, candidate) {
				return true
			}
		}
		return false
	case OpGt, OpGte, OpLt, OpLte:
		got, okGot := ToFloat64(value)
		want, okWant := ToFloat64(c.Value)
		if !okGot || !okWant {
			return false
		}
		switch c.Op {
		case OpGt:
			return got > want
		case OpGte:
			return got >= want
		case OpLt:
			return got < want
		default:
			return got <= want
		}
	default:
		return false
	}
}

// valuesEqual compares two payload/condition values, normalizing numbers to
// float64. Only strings, booleans and numbers are comparable; any other type
// never matches.
func valuesEqual(a, b any) bool {
	if fa, ok := ToFloat64(a); ok {
		fb, okB := ToFloat64(b)
		return okB && fa == fb
	}

	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return false
	}
}

// ToFloat64 normalizes any numeric value to float64. It reports false for
// non-numeric values.
func ToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}
