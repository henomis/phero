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
	"database/sql"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	qdrantapi "github.com/qdrant/go-client/qdrant"

	embeddingOpenAI "github.com/henomis/phero/embedding/openai"

	// PostgreSQL driver registered as "pgx".
	_ "github.com/jackc/pgx/v5/stdlib"

	llmanthropic "github.com/henomis/phero/llm/anthropic"
	llmopenai "github.com/henomis/phero/llm/openai"
)

// ---- default constants -------------------------------------------------

const (
	defaultOpenAIKey      = "ollama"
	defaultOpenAIBaseURL  = "http://localhost:11434/v1"
	defaultOpenAIModel    = "minimax-m2.7:cloud"
	defaultAnthropicKey   = "ollama"
	defaultAnthropicURL   = "http://localhost:11434"
	defaultAnthropicModel = "minimax-m2.7:cloud"
	defaultEmbedModel     = "nomic-embed-text"
	defaultPostgresDSN    = "postgres://phero:phero@localhost:5432/phero?sslmode=disable"
	defaultQdrantHost     = "localhost"
	defaultQdrantPort     = 6334
)

// ---- env helpers -------------------------------------------------------

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

func openAIKey() string     { return envOr("OPENAI_API_KEY", defaultOpenAIKey) }
func openAIBaseURL() string { return envOr("OPENAI_BASE_URL", defaultOpenAIBaseURL) }
func openAIModel() string   { return envOr("OPENAI_MODEL", defaultOpenAIModel) }

func anthropicKey() string     { return envOr("ANTHROPIC_AUTH_TOKEN", defaultAnthropicKey) }
func anthropicBaseURL() string { return envOr("ANTHROPIC_BASE_URL", defaultAnthropicURL) }
func anthropicModel() string   { return envOr("ANTHROPIC_MODEL", defaultAnthropicModel) }

func embedModel() string { return envOr("EMBEDDING_MODEL", defaultEmbedModel) }

func postgresDSN() string { return envOr("POSTGRES_DSN", defaultPostgresDSN) }

func qdrantHostEnv() string { return envOr("QDRANT_HOST", defaultQdrantHost) }

func qdrantPortEnv() int {
	s := envOr("QDRANT_PORT", strconv.Itoa(defaultQdrantPort))

	p, err := strconv.Atoi(s)
	if err != nil {
		return defaultQdrantPort
	}

	return p
}

// ---- client builders ---------------------------------------------------

// buildOpenAILLM returns an OpenAI-compatible LLM client pointed at Ollama.
func buildOpenAILLM() *llmopenai.Client {
	return llmopenai.New(
		openAIKey(),
		llmopenai.WithBaseURL(openAIBaseURL()),
		llmopenai.WithModel(openAIModel()),
	)
}

// buildAnthropicLLM returns an Anthropic-compatible LLM client pointed at Ollama.
func buildAnthropicLLM() *llmanthropic.Client {
	return llmanthropic.New(
		anthropicKey(),
		llmanthropic.WithBaseURL(anthropicBaseURL()),
		llmanthropic.WithModel(anthropicModel()),
		llmanthropic.WithMaxTokens(1024),
	)
}

// buildEmbedder returns an OpenAI-compatible embedder pointed at Ollama.
func buildEmbedder() *embeddingOpenAI.Client {
	return embeddingOpenAI.New(
		openAIKey(),
		embeddingOpenAI.WithBaseURL(openAIBaseURL()),
		embeddingOpenAI.WithModel(embedModel()),
	)
}

// ---- service availability helpers -------------------------------------

// requirePostgres skips the test if PostgreSQL is not reachable and
// returns an open *sql.DB on success. The DB is closed via t.Cleanup.
func requirePostgres(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", postgresDSN())
	if err != nil {
		t.Skipf("postgres: cannot open connection: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("postgres not reachable (%s): %v", postgresDSN(), err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

// requireQdrant skips the test if Qdrant is not reachable and returns a
// connected *qdrantapi.Client on success.
func requireQdrant(t *testing.T) *qdrantapi.Client {
	t.Helper()

	addr := fmt.Sprintf("%s:%d", qdrantHostEnv(), qdrantPortEnv())

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Skipf("qdrant not reachable at %s: %v", addr, err)
	}

	conn.Close()

	c, err := qdrantapi.NewClient(&qdrantapi.Config{
		Host: qdrantHostEnv(),
		Port: qdrantPortEnv(),
	})
	if err != nil {
		t.Skipf("qdrant: cannot create client: %v", err)
	}

	return c
}

// probeVectorSize embeds a single string to discover the model's output dimension.
// The test is skipped if embedding fails.
func probeVectorSize(t *testing.T) uint64 {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vecs, err := buildEmbedder().Embed(ctx, []string{"dimension probe"})
	if err != nil {
		t.Skipf("embedding probe failed: %v", err)
	}

	if len(vecs) == 0 || len(vecs[0]) == 0 {
		t.Skip("embedding probe returned empty vector")
	}

	return uint64(len(vecs[0]))
}
