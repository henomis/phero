package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/henomis/phero/embedding"
)

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Object string          `json:"object"`
	Data   []embeddingItem `json:"data"`
	Model  string          `json:"model"`
}

type embeddingItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

func TestClient_Embed_BatchingAndOrdering(t *testing.T) {
	var calls int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)

		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("expected path /v1/embeddings, got %s", r.URL.Path)
		}

		var req embeddingsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-model" {
			t.Fatalf("expected model test-model, got %q", req.Model)
		}
		if len(req.Input) == 0 {
			t.Fatalf("expected non-empty input")
		}

		// Return out-of-order data to ensure the client uses the Index field.
		items := make([]embeddingItem, 0, len(req.Input))
		for i := len(req.Input) - 1; i >= 0; i-- {
			items = append(items, embeddingItem{
				Object:    "embedding",
				Index:     i,
				Embedding: []float32{float32(len(req.Input[i])), float32(len(req.Input[i]))},
			})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(embeddingsResponse{
			Object: "list",
			Data:   items,
			Model:  req.Model,
		})
	}))
	defer srv.Close()

	c := New("test-key",
		WithBaseURL(srv.URL+"/v1"),
		WithModel("test-model"),
	)

	texts := []string{"a", "bb", "ccc", "dddd", "eeeee"}
	vecs, err := c.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed error: %v", err)
	}

	if got, want := atomic.LoadInt32(&calls), int32(3); got != want {
		t.Fatalf("expected %d requests, got %d", want, got)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("expected %d vectors, got %d", len(texts), len(vecs))
	}

	for i := range texts {
		if len(vecs[i]) != 2 {
			t.Fatalf("expected vector length 2 at %d, got %d", i, len(vecs[i]))
		}
		if vecs[i][0] != float32(len(texts[i])) {
			t.Fatalf("unexpected vec[%d][0]=%v, want %v", i, vecs[i][0], float32(len(texts[i])))
		}
		if vecs[i][1] != float32(len(texts[i])) {
			t.Fatalf("unexpected vec[%d][1]=%v, want %v", i, vecs[i][1], float32(len(texts[i])))
		}
	}
}

func TestClient_Embed_EmptyInput(t *testing.T) {
	c := New("test-key")
	_, err := c.Embed(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err != embedding.ErrEmptyInput {
		t.Fatalf("expected ErrEmptyInput, got %v", err)
	}
}
