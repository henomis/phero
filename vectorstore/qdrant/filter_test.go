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
	"errors"
	"testing"

	qdrantapi "github.com/qdrant/go-client/qdrant"

	"github.com/henomis/phero/vectorstore"
)

func TestTranslateFilterNil(t *testing.T) {
	f, err := translateFilter(nil)
	if err != nil {
		t.Fatalf("translateFilter(nil) error = %v", err)
	}
	if f != nil {
		t.Fatalf("translateFilter(nil) = %v, want nil", f)
	}
}

func TestTranslateFilterEq(t *testing.T) {
	f, err := translateFilter(vectorstore.NewFilter(
		vectorstore.Eq("category", "news"),
		vectorstore.Eq("draft", false),
		vectorstore.Eq("year", 2024),
		vectorstore.Eq("score", 0.5),
	))
	if err != nil {
		t.Fatalf("translateFilter() error = %v", err)
	}
	if len(f.Must) != 4 {
		t.Fatalf("len(Must) = %d, want 4", len(f.Must))
	}

	if kw := f.Must[0].GetField().GetMatch().GetKeyword(); kw != "news" {
		t.Fatalf("keyword = %q, want news", kw)
	}
	if b := f.Must[1].GetField().GetMatch().GetBoolean(); b != false {
		t.Fatalf("boolean = %v, want false", b)
	}
	if n := f.Must[2].GetField().GetMatch().GetInteger(); n != 2024 {
		t.Fatalf("integer = %d, want 2024", n)
	}
	// Float equality maps to a closed range [v, v].
	r := f.Must[3].GetField().GetRange()
	if r.GetGte() != 0.5 || r.GetLte() != 0.5 {
		t.Fatalf("range = %v, want [0.5, 0.5]", r)
	}
}

func TestTranslateFilterNe(t *testing.T) {
	f, err := translateFilter(vectorstore.NewFilter(vectorstore.Ne("category", "news")))
	if err != nil {
		t.Fatalf("translateFilter() error = %v", err)
	}
	if len(f.MustNot) != 2 {
		t.Fatalf("len(MustNot) = %d, want 2 (match + is-empty)", len(f.MustNot))
	}
	if kw := f.MustNot[0].GetField().GetMatch().GetKeyword(); kw != "news" {
		t.Fatalf("keyword = %q, want news", kw)
	}
	if f.MustNot[1].GetIsEmpty().GetKey() != "category" {
		t.Fatalf("is-empty key = %q, want category", f.MustNot[1].GetIsEmpty().GetKey())
	}
}

func TestTranslateFilterIn(t *testing.T) {
	f, err := translateFilter(vectorstore.NewFilter(vectorstore.In("year", 2020, 2024)))
	if err != nil {
		t.Fatalf("translateFilter() error = %v", err)
	}
	if len(f.Must) != 1 {
		t.Fatalf("len(Must) = %d, want 1", len(f.Must))
	}
	nested := f.Must[0].GetFilter()
	if nested == nil || len(nested.Should) != 2 {
		t.Fatalf("nested should = %v, want 2 conditions", nested)
	}
	if n := nested.Should[0].GetField().GetMatch().GetInteger(); n != 2020 {
		t.Fatalf("integer = %d, want 2020", n)
	}
}

func TestTranslateFilterRange(t *testing.T) {
	tests := []struct {
		name  string
		cond  vectorstore.Condition
		check func(r *qdrantapi.Range) bool
	}{
		{"gt", vectorstore.Gt("year", 2020), func(r *qdrantapi.Range) bool { return r.GetGt() == 2020 }},
		{"gte", vectorstore.Gte("year", 2020), func(r *qdrantapi.Range) bool { return r.GetGte() == 2020 }},
		{"lt", vectorstore.Lt("year", 2020), func(r *qdrantapi.Range) bool { return r.GetLt() == 2020 }},
		{"lte", vectorstore.Lte("year", 2020), func(r *qdrantapi.Range) bool { return r.GetLte() == 2020 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := translateFilter(vectorstore.NewFilter(tt.cond))
			if err != nil {
				t.Fatalf("translateFilter() error = %v", err)
			}
			if len(f.Must) != 1 {
				t.Fatalf("len(Must) = %d, want 1", len(f.Must))
			}
			r := f.Must[0].GetField().GetRange()
			if r == nil || !tt.check(r) {
				t.Fatalf("range = %v, unexpected bounds", r)
			}
		})
	}
}

func TestTranslateFilterInvalid(t *testing.T) {
	_, err := translateFilter(vectorstore.NewFilter(vectorstore.Gt("year", "not-a-number")))
	if !errors.Is(err, vectorstore.ErrInvalidFilter) {
		t.Fatalf("translateFilter() error = %v, want ErrInvalidFilter", err)
	}
}
