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

import (
	"errors"
	"testing"
)

func TestMatchPayload(t *testing.T) {
	payload := map[string]any{
		"category": "news",
		"year":     int64(2024),
		"score":    0.5,
		"draft":    false,
	}

	tests := []struct {
		name   string
		filter *Filter
		want   bool
	}{
		{name: "nil filter matches", filter: nil, want: true},
		{name: "empty filter matches", filter: NewFilter(), want: true},
		{name: "eq string match", filter: NewFilter(Eq("category", "news")), want: true},
		{name: "eq string mismatch", filter: NewFilter(Eq("category", "sports")), want: false},
		{name: "eq bool match", filter: NewFilter(Eq("draft", false)), want: true},
		{name: "eq numeric cross-type", filter: NewFilter(Eq("year", 2024)), want: true},
		{name: "eq float cross-type", filter: NewFilter(Eq("score", float32(0.5))), want: true},
		{name: "eq missing key", filter: NewFilter(Eq("missing", "x")), want: false},
		{name: "eq type mismatch", filter: NewFilter(Eq("category", 7)), want: false},
		{name: "ne match", filter: NewFilter(Ne("category", "sports")), want: true},
		{name: "ne same value", filter: NewFilter(Ne("category", "news")), want: false},
		{name: "ne missing key never matches", filter: NewFilter(Ne("missing", "x")), want: false},
		{name: "in match", filter: NewFilter(In("category", "sports", "news")), want: true},
		{name: "in mismatch", filter: NewFilter(In("category", "sports", "politics")), want: false},
		{name: "in numeric", filter: NewFilter(In("year", 2020, 2024)), want: true},
		{name: "gt match", filter: NewFilter(Gt("year", 2020)), want: true},
		{name: "gt boundary", filter: NewFilter(Gt("year", 2024)), want: false},
		{name: "gte boundary", filter: NewFilter(Gte("year", 2024)), want: true},
		{name: "lt match", filter: NewFilter(Lt("score", 1)), want: true},
		{name: "lte boundary", filter: NewFilter(Lte("score", 0.5)), want: true},
		{name: "range on string never matches", filter: NewFilter(Gt("category", 1)), want: false},
		{name: "and semantics all match", filter: NewFilter(Eq("category", "news"), Gt("year", 2020)), want: true},
		{name: "and semantics one fails", filter: NewFilter(Eq("category", "news"), Gt("year", 2030)), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchPayload(tt.filter, payload); got != tt.want {
				t.Fatalf("MatchPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterValidate(t *testing.T) {
	tests := []struct {
		name    string
		filter  *Filter
		wantErr error
	}{
		{name: "nil filter", filter: nil, wantErr: nil},
		{name: "valid", filter: NewFilter(Eq("a", 1), In("b", "x"), Gt("c", 2)), wantErr: nil},
		{name: "empty key", filter: NewFilter(Eq("", 1)), wantErr: ErrInvalidFilter},
		{name: "eq nil value", filter: NewFilter(Eq("a", nil)), wantErr: ErrInvalidFilter},
		{name: "in no values", filter: NewFilter(In("a")), wantErr: ErrInvalidFilter},
		{name: "range non-numeric", filter: NewFilter(Gt("a", "x")), wantErr: ErrInvalidFilter},
		{name: "unknown op", filter: NewFilter(Condition{Key: "a", Op: Op("like"), Value: "x"}), wantErr: ErrUnsupportedFilterOp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyQueryOptions(t *testing.T) {
	f := NewFilter(Eq("a", 1))

	cfg := ApplyQueryOptions([]QueryOption{WithFilter(f), nil})
	if cfg.Filter != f {
		t.Fatalf("ApplyQueryOptions() filter = %v, want %v", cfg.Filter, f)
	}

	empty := ApplyQueryOptions(nil)
	if empty.Filter != nil {
		t.Fatalf("ApplyQueryOptions(nil) filter = %v, want nil", empty.Filter)
	}
}

func TestToFloat64(t *testing.T) {
	if v, ok := ToFloat64(int32(7)); !ok || v != 7 {
		t.Fatalf("ToFloat64(int32(7)) = %v, %v", v, ok)
	}
	if v, ok := ToFloat64(uint8(3)); !ok || v != 3 {
		t.Fatalf("ToFloat64(uint8(3)) = %v, %v", v, ok)
	}
	if _, ok := ToFloat64("7"); ok {
		t.Fatal("ToFloat64(string) should not be numeric")
	}
	if _, ok := ToFloat64(nil); ok {
		t.Fatal("ToFloat64(nil) should not be numeric")
	}
}
