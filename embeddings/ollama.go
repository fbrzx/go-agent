package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ollamaEmbedder struct {
	host      string
	model     string
	dimension int
	client    *http.Client
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float64 `json:"embedding"`
}

func NewOllamaEmbedder(opts Options) Embedder {
	host := strings.TrimRight(opts.OllamaHost, "/")
	if host == "" {
		host = "http://localhost:11434"
	}

	return &ollamaEmbedder{
		host:      host,
		model:     opts.Model,
		dimension: opts.Dimension,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, 0, len(texts))

	url := fmt.Sprintf("%s/api/embeddings", e.host)

	for _, text := range texts {
		reqBody, err := json.Marshal(ollamaRequest{Model: e.model, Prompt: text})
		if err != nil {
			return nil, fmt.Errorf("marshal ollama request: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("create ollama request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := e.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("call ollama embeddings API: %w", err)
		}

		var payload ollamaResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode ollama response: %w", err)
		}
		resp.Body.Close()

		vec := make([]float32, len(payload.Embedding))
		for i, value := range payload.Embedding {
			vec[i] = float32(value)
		}

		if e.dimension > 0 && len(vec) != e.dimension {
			return nil, fmt.Errorf("ollama embedding dimension mismatch: expected %d, got %d", e.dimension, len(vec))
		}

		results = append(results, vec)
	}

	return results, nil
}
