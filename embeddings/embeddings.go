package embeddings

import (
	"context"
	"fmt"

	"github.com/fabfab/go-agent/config"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type Options struct {
	Provider  string
	Model     string
	Dimension int

	OllamaHost    string
	OpenAIAPIKey  string
	OpenAIBaseURL string
}

func NewEmbedder(cfg config.Config) (Embedder, error) {
	opts := Options{
		Provider:      cfg.Embeddings.Provider,
		Model:         cfg.Embeddings.Model,
		Dimension:     cfg.Embeddings.Dimension,
		OllamaHost:    cfg.OllamaHost,
		OpenAIAPIKey:  cfg.OpenAIAPIKey,
		OpenAIBaseURL: cfg.OpenAIBaseURL,
	}

	switch opts.Provider {
	case config.ProviderOllama:
		return NewOllamaEmbedder(opts), nil
	case config.ProviderOpenAI:
		if opts.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("openai provider selected but OPENAI_API_KEY not set")
		}
		return NewOpenAIEmbedder(opts), nil
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", opts.Provider)
	}
}
