// Package llm provides language model client interfaces for Ollama and OpenAI.
package llm

import (
	"context"
	"fmt"

	"github.com/fabfab/go-agent/config"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client interface {
	Generate(ctx context.Context, messages []Message) (string, error)
}

// StreamClient extends Client with streaming support.
// Implementations should invoke the callback with incremental chunks as they
// are produced, returning early if the callback reports an error.
type StreamClient interface {
	Client
	GenerateStream(ctx context.Context, messages []Message, fn func(string) error) error
}

type Options struct {
	Provider string
	Model    string

	OllamaHost    string
	OpenAIAPIKey  string
	OpenAIBaseURL string
}

func NewClient(cfg config.Config) (Client, error) {
	opts := Options{
		Provider:      cfg.LLM.Provider,
		Model:         cfg.LLM.Model,
		OllamaHost:    cfg.OllamaHost,
		OpenAIAPIKey:  cfg.OpenAIAPIKey,
		OpenAIBaseURL: cfg.OpenAIBaseURL,
	}

	switch opts.Provider {
	case config.ProviderOllama:
		return NewOllamaClient(opts), nil
	case config.ProviderOpenAI:
		if opts.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("openai provider selected but OPENAI_API_KEY not set")
		}
		return NewOpenAIClient(opts), nil
	default:
		return nil, fmt.Errorf("unknown llm provider: %s", opts.Provider)
	}
}
