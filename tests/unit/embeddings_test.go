package unit

import (
	"testing"

	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/embeddings"
)

func TestNewEmbedderDefaults(t *testing.T) {
	cfg := config.Config{
		Embeddings: config.EmbeddingConfig{
			Provider:  config.ProviderOllama,
			Model:     "nomic-embed-text",
			Dimension: 3,
		},
		OllamaHost: "http://localhost:11434",
	}

	embedder, err := embeddings.NewEmbedder(cfg)
	if err != nil {
		t.Fatalf("expected embedder, got error: %v", err)
	}

	if embedder == nil {
		t.Fatal("expected non-nil embedder")
	}
}

func TestNewEmbedderOpenAIMissingKey(t *testing.T) {
	cfg := config.Config{
		Embeddings: config.EmbeddingConfig{
			Provider:  config.ProviderOpenAI,
			Model:     "text-embedding-3-small",
			Dimension: 1536,
		},
	}

	if _, err := embeddings.NewEmbedder(cfg); err == nil {
		t.Fatal("expected error for missing OPENAI_API_KEY")
	}
}
