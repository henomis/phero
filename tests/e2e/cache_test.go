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

//go:build e2e

package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/henomis/phero/embedding"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/llm/middleware"
	"github.com/henomis/phero/vectorstore"
	vspsql "github.com/henomis/phero/vectorstore/psql"
	vsqdrant "github.com/henomis/phero/vectorstore/qdrant"
	vsweaviate "github.com/henomis/phero/vectorstore/weaviate"
)

// constEmbedder embeds every text to the same vector, so every lookup is a
// perfect similarity match and only the call contract can tell entries apart.
type constEmbedder struct{}

func (constEmbedder) Embed(_ context.Context, texts []string) ([]embedding.Vector, error) {
	out := make([]embedding.Vector, len(texts))
	for i := range texts {
		out[i] = embedding.Vector{1, 0, 0, 0}
	}

	return out, nil
}

// TestSemanticCache_ContractFilter runs the semantic cache against each real
// vector store and checks that entries are only served to calls with the same
// response format, which relies on the store applying the payload filter.
func TestSemanticCache_ContractFilter(t *testing.T) {
	stores := map[string]func(t *testing.T) vectorstore.Store{
		"qdrant": func(t *testing.T) vectorstore.Store {
			s, err := vsqdrant.New(requireQdrant(t), "e2e-cache-"+uuid.NewString(), vsqdrant.WithVectorSize(4))
			if err != nil {
				t.Fatalf("vsqdrant.New: %v", err)
			}

			return s
		},
		"psql": func(t *testing.T) vectorstore.Store {
			s, err := vspsql.New(requirePostgres(t), "e2e_cache_"+uuid.NewString()[:8],
				vspsql.WithVectorSize(4), vspsql.WithEnsureExtension(true))
			if err != nil {
				t.Fatalf("vspsql.New: %v", err)
			}

			return s
		},
		"weaviate": func(t *testing.T) vectorstore.Store {
			s, err := vsweaviate.New(requireWeaviate(t), "E2ecache"+uuid.NewString()[:8], vsweaviate.WithVectorSize(4))
			if err != nil {
				t.Fatalf("vsweaviate.New: %v", err)
			}

			return s
		},
	}

	format, err := llm.NewResponseFormat[cityFacts]("city_facts", "")
	if err != nil {
		t.Fatalf("NewResponseFormat: %v", err)
	}

	for name, build := range stores {
		t.Run(name, func(t *testing.T) {
			store := build(t)

			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				_ = store.Clear(ctx)
			})

			calls := 0
			inner := llm.Func(func(context.Context, []llm.Message, ...llm.CallOption) (*llm.Result, error) {
				calls++
				msg := llm.AssistantMessage([]llm.ContentPart{llm.Text("answer")})

				return &llm.Result{Message: &msg, Usage: &llm.Usage{}}, nil
			})

			mw, mwErr := middleware.NewSemanticCache(constEmbedder{}, store)
			if mwErr != nil {
				t.Fatalf("NewSemanticCache: %v", mwErr)
			}

			client := llm.Use(inner, mw)
			msgs := []llm.Message{llm.UserMessage(llm.Text("hello"))}

			steps := []struct {
				name      string
				opts      []llm.CallOption
				wantCalls int
			}{
				{name: "free text, miss", wantCalls: 1},
				{name: "structured, miss", opts: []llm.CallOption{llm.WithResponseFormat(format)}, wantCalls: 2},
				{name: "structured, hit", opts: []llm.CallOption{llm.WithResponseFormat(format)}, wantCalls: 2},
				{name: "free text, hit", wantCalls: 2},
			}

			for _, step := range steps {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				_, execErr := client.Execute(ctx, msgs, step.opts...)

				cancel()

				if execErr != nil {
					t.Fatalf("%s: Execute: %v", step.name, execErr)
				}

				if calls != step.wantCalls {
					t.Fatalf("%s: model calls = %d, want %d", step.name, calls, step.wantCalls)
				}
			}
		})
	}
}
