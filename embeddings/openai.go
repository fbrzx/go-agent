package embeddings

import (
	"context"
	"fmt"

	openai "github.com/sashabaranov/go-openai"
)

type openAIEmbedder struct {
	client    *openai.Client
	model     string
	dimension int
}

func NewOpenAIEmbedder(opts Options) Embedder {
	cfg := openai.DefaultConfig(opts.OpenAIAPIKey)
	if opts.OpenAIBaseURL != "" {
		cfg.BaseURL = opts.OpenAIBaseURL
	}

	return &openAIEmbedder{
		client:    openai.NewClientWithConfig(cfg),
		model:     opts.Model,
		dimension: opts.Dimension,
	}
}

func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Model: openai.EmbeddingModel(e.model),
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("create openai embeddings: %w", err)
	}

	results := make([][]float32, len(resp.Data))
	for i, datum := range resp.Data {
		if e.dimension > 0 && len(datum.Embedding) != e.dimension {
			return nil, fmt.Errorf("openai embedding dimension mismatch: expected %d, got %d", e.dimension, len(datum.Embedding))
		}
		results[i] = datum.Embedding
	}

	return results, nil
}
