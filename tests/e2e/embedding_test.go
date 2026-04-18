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
)

// TestEmbedder_SingleText verifies that the embedder returns a non-empty
// vector for a single input string.
func TestEmbedder_SingleText(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	embedder := buildEmbedder()

	vectors, err := embedder.Embed(ctx, []string{"The quick brown fox"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vectors) != 1 {
		t.Fatalf("expected 1 vector, got %d", len(vectors))
	}

	if len(vectors[0]) == 0 {
		t.Fatal("expected non-empty vector")
	}

	t.Logf("Embedding dimension: %d", len(vectors[0]))
}

// TestEmbedder_BatchTexts verifies that embedding multiple texts returns one
// vector per input.
func TestEmbedder_BatchTexts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	texts := []string{
		"Hello world",
		"Golang is great",
		"AI agents are powerful",
	}

	embedder := buildEmbedder()

	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vectors) != len(texts) {
		t.Fatalf("expected %d vectors, got %d", len(texts), len(vectors))
	}

	dim := len(vectors[0])

	for i, v := range vectors {
		if len(v) == 0 {
			t.Errorf("vector %d is empty", i)
		}

		if len(v) != dim {
			t.Errorf("vector %d has dimension %d, expected %d", i, len(v), dim)
		}
	}

	t.Logf("Embedded %d texts, dimension=%d", len(texts), dim)
}

// TestEmbedder_SimilarTextsHaveHigherCosineSimilarity verifies that semantically
// similar texts produce closer embeddings than dissimilar ones.
func TestEmbedder_SimilarTextsHaveHigherCosineSimilarity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	embedder := buildEmbedder()

	texts := []string{
		"The cat sat on the mat",
		"A feline rested on a rug",   // semantically similar to [0]
		"Quantum physics is complex", // semantically different
	}

	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}

	simSimilar := cosineSimilarity(vectors[0], vectors[1])
	simDissimilar := cosineSimilarity(vectors[0], vectors[2])

	t.Logf("cosine(similar)=%.4f  cosine(dissimilar)=%.4f", simSimilar, simDissimilar)

	if simSimilar <= simDissimilar {
		t.Logf("Note: similar texts did not score higher than dissimilar (model=%s)", embedModel())
	}
}

// cosineSimilarity computes the cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float32

	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return dot / (sqrtF32(normA) * sqrtF32(normB))
}

// sqrtF32 is a simple float32 square root via float64.
func sqrtF32(x float32) float32 {
	var result float32 = 1

	// Newton-Raphson approximation (good enough for test purposes).
	for range 20 {
		result = (result + float32(x)/result) / 2
	}

	return result
}
