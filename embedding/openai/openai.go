package openai

import (
	"context"

	openaiapi "github.com/sashabaranov/go-openai"

	"github.com/henomis/phero/embedding"
)

// Client implements embedding.Embedder using the OpenAI Embeddings API.
var _ embedding.Embedder = (*Client)(nil)

const (
	// DefaultModel is the model used when no explicit model option is provided.
	DefaultModel = openaiapi.SmallEmbedding3
	// OllamaBaseURL is the OpenAI-compatible base URL used by the local Ollama server.
	OllamaBaseURL = "http://localhost:11434/v1"
)

// Client is an embedding.Embedder implementation that uses github.com/sashabaranov/go-openai.
type Client struct {
	client *openaiapi.Client

	model  openaiapi.EmbeddingModel
	apiKey string
	config openaiapi.ClientConfig
}

// Option configures a Client created by New.
type Option func(*Client)

// New constructs a new Client with the given API key and applies any options.
//
// By default it uses DefaultModel and the standard OpenAI base URL from
// go-openai's DefaultConfig.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey: apiKey,
		model:  DefaultModel,
		config: openaiapi.DefaultConfig(apiKey),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	c.client = openaiapi.NewClientWithConfig(c.config)
	return c
}

// Embed generates embeddings for the given input texts.
func (c *Client) Embed(ctx context.Context, texts []string) ([]embedding.Vector, error) {
	request := openaiapi.EmbeddingRequest{
		Model: c.model,
		Input: texts,
	}

	response, err := c.client.CreateEmbeddings(ctx, request)
	if err != nil {
		return nil, err
	}

	vecs := make([]embedding.Vector, len(texts))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(vecs) {
			return nil, &ResponseIndexOutOfRangeError{Index: item.Index, Len: len(vecs)}
		}
		vecs[item.Index] = item.Embedding
	}

	for i := range vecs {
		if vecs[i] == nil {
			return nil, &MissingEmbeddingError{Index: i}
		}
	}

	return vecs, nil
}

// WithBaseURL sets the base URL used by the underlying OpenAI client.
//
// This enables use with OpenAI-compatible endpoints.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.config.BaseURL = baseURL
		}
	}
}

// WithModel sets the model name used for embeddings.
func WithModel(model string) Option {
	return func(c *Client) {
		if model != "" {
			c.model = openaiapi.EmbeddingModel(model)
		}
	}
}

// WithOllamaBaseURL configures the client to use the default local Ollama base URL.
func WithOllamaBaseURL() Option {
	return WithBaseURL(OllamaBaseURL)
}
