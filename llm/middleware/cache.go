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

package middleware

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/henomis/phero/embedding"
	"github.com/henomis/phero/llm"
	"github.com/henomis/phero/vectorstore"
)

const (
	// defaultSimilarityThreshold is the minimum cosine similarity for a cache hit.
	defaultSimilarityThreshold = 0.95
	// cachePayloadKey is the vectorstore payload key holding the serialized result.
	cachePayloadKey = "result"
)

// SemanticCacheOption configures a SemanticCache middleware.
type SemanticCacheOption func(*semanticCacheConfig)

type semanticCacheConfig struct {
	threshold     float32
	reportUsage   bool
	skipWithTools bool
}

// WithSimilarityThreshold sets the minimum cosine similarity (0..1) a stored
// entry must reach to be served from the cache. Higher is stricter.
// Default: 0.95.
func WithSimilarityThreshold(threshold float32) SemanticCacheOption {
	return func(c *semanticCacheConfig) { c.threshold = threshold }
}

// WithReportCachedUsage controls the Usage reported on a cache hit. By default
// a hit reports zero tokens (no model call was made), which keeps cost
// accounting truthful. Enable this to instead return the usage recorded when
// the entry was first stored.
func WithReportCachedUsage(report bool) SemanticCacheOption {
	return func(c *semanticCacheConfig) { c.reportUsage = report }
}

// WithSkipToolCalls controls whether requests that include tools bypass the
// cache entirely. Replaying a cached assistant turn that carries tool calls
// makes the agent re-execute those tools, which may not be safe. By default
// such requests are not cached. Disable this to cache them anyway.
func WithSkipToolCalls(skip bool) SemanticCacheOption {
	return func(c *semanticCacheConfig) { c.skipWithTools = skip }
}

// NewSemanticCache returns an llm.LLMMiddleware that caches LLM responses keyed
// by the semantic similarity of the conversation.
//
// On each Execute the conversation is embedded with embedder and the nearest
// neighbour is looked up in store; if its cosine similarity meets the
// configured threshold, the stored response is returned without calling the
// underlying LLM. Otherwise the inner LLM is called and its response is
// embedded and persisted for future hits.
//
//	cacheMW, err := middleware.NewSemanticCache(embedder, store)
//	if err != nil { ... }
//	client := llm.Use(base, cacheMW)
func NewSemanticCache(embedder embedding.Embedder, store vectorstore.Store, opts ...SemanticCacheOption) (llm.LLMMiddleware, error) {
	if embedder == nil {
		return nil, ErrNilEmbedder
	}

	if store == nil {
		return nil, ErrNilStore
	}

	cfg := &semanticCacheConfig{
		threshold:     defaultSimilarityThreshold,
		reportUsage:   false,
		skipWithTools: true,
	}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.threshold < 0 || cfg.threshold > 1 {
		return nil, ErrInvalidThreshold
	}

	return func(next llm.LLM) llm.LLM {
		return &semanticCacheLLM{
			inner:    next,
			embedder: embedder,
			store:    store,
			cfg:      cfg,
		}
	}, nil
}

// semanticCacheLLM is the concrete LLM produced by the SemanticCache middleware.
type semanticCacheLLM struct {
	inner    llm.LLM
	embedder embedding.Embedder
	store    vectorstore.Store
	cfg      *semanticCacheConfig

	ensure sync.Once
}

// Execute serves a cached response when a sufficiently similar conversation has
// been seen, otherwise delegates to the inner LLM and stores the response.
//
// Cache failures (embedding, store) never block a call: they degrade to a
// normal, uncached Execute.
func (s *semanticCacheLLM) Execute(ctx context.Context, messages []llm.Message, tools []*llm.Tool) (*llm.Result, error) {
	if s.cfg.skipWithTools && len(tools) > 0 {
		return s.inner.Execute(ctx, messages, tools)
	}

	if err := s.ensureCollection(ctx); err != nil {
		return s.inner.Execute(ctx, messages, tools)
	}

	key := cacheKey(messages, tools)

	vectors, err := s.embedder.Embed(ctx, []string{key})
	if err != nil || len(vectors) == 0 {
		return s.inner.Execute(ctx, messages, tools)
	}

	vector := vectors[0]

	if cached := s.lookup(ctx, vector); cached != nil {
		return cached, nil
	}

	result, err := s.inner.Execute(ctx, messages, tools)
	if err != nil {
		return nil, err
	}

	s.persist(ctx, vector, result)

	return result, nil
}

// ensureCollection ensures the backing collection exists, at most once.
func (s *semanticCacheLLM) ensureCollection(ctx context.Context) error {
	var ensureErr error

	s.ensure.Do(func() { ensureErr = s.store.EnsureCollection(ctx) })

	return ensureErr
}

// lookup returns a cached result when the nearest neighbour meets the
// configured similarity threshold, or nil on a miss or any failure.
func (s *semanticCacheLLM) lookup(ctx context.Context, vector vectorstore.Vector) *llm.Result {
	scored, err := s.store.Query(ctx, vector, 1)
	if err != nil || len(scored) == 0 {
		return nil
	}

	best := scored[0]
	if best.Score < s.cfg.threshold {
		return nil
	}

	raw, ok := best.Payload[cachePayloadKey].(string)
	if !ok {
		return nil
	}

	var result llm.Result
	if jsonErr := json.Unmarshal([]byte(raw), &result); jsonErr != nil {
		return nil
	}

	if !s.cfg.reportUsage {
		result.Usage = &llm.Usage{}
	}

	return &result
}

// persist stores a result against its conversation embedding for future hits.
// Failures are silently ignored so caching never breaks a successful call.
func (s *semanticCacheLLM) persist(ctx context.Context, vector vectorstore.Vector, result *llm.Result) {
	if result == nil {
		return
	}

	raw, err := json.Marshal(result)
	if err != nil {
		return
	}

	_ = s.store.Upsert(ctx, []vectorstore.Point{{
		ID:      uuid.NewString(),
		Vector:  vector,
		Payload: map[string]any{cachePayloadKey: string(raw)},
	}})
}

// cacheKey builds a stable textual representation of the request used as the
// embedding input. It includes every message's role and text plus the sorted
// tool names so semantically equivalent conversations map to nearby vectors.
func cacheKey(messages []llm.Message, tools []*llm.Tool) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Role)
		sb.WriteByte(':')
		sb.WriteString(m.TextContent())
		sb.WriteByte('\n')
	}

	if len(tools) > 0 {
		names := make([]string, 0, len(tools))
		for _, t := range tools {
			if t != nil {
				names = append(names, t.Name())
			}
		}

		sort.Strings(names)
		sb.WriteString("tools:")
		sb.WriteString(strings.Join(names, ","))
	}

	return sb.String()
}
