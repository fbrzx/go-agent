package unit

import (
	"testing"

	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/llm"
)

func TestNewClientDefaults(t *testing.T) {
	cfg := config.Config{
		LLM: config.LLMConfig{
			Provider: config.ProviderOllama,
			Model:    "llama3.1:8b",
		},
		OllamaHost: "http://localhost:11434",
	}

	client, err := llm.NewClient(cfg)
	if err != nil {
		t.Fatalf("expected llm client, got error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientOpenAIRequiresAPIKey(t *testing.T) {
	cfg := config.Config{
		LLM: config.LLMConfig{
			Provider: config.ProviderOpenAI,
			Model:    "gpt-4o",
		},
	}

	if _, err := llm.NewClient(cfg); err == nil {
		t.Fatal("expected error for missing OPENAI_API_KEY")
	}
}
