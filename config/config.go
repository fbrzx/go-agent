package config

import (
	"os"
	"strconv"
)

const (
	ProviderOllama = "ollama"
	ProviderOpenAI = "openai"
)

type Config struct {
	PostgresDSN string
	Neo4jURI    string
	Neo4jUser   string
	Neo4jPass   string

	DataDir string

	OllamaHost    string
	OpenAIAPIKey  string
	OpenAIBaseURL string

	Embeddings EmbeddingConfig
	LLM        LLMConfig
}

type EmbeddingConfig struct {
	Provider  string
	Model     string
	Dimension int
}

type LLMConfig struct {
	Provider string
	Model    string
}

func Load() Config {
	return Config{
		PostgresDSN:   getEnv("POSTGRES_DSN", "postgres://localhost:5432/go-agent?sslmode=disable"),
		Neo4jURI:      getEnv("NEO4J_URI", "neo4j://localhost:7687"),
		Neo4jUser:     getEnv("NEO4J_USERNAME", "neo4j"),
		Neo4jPass:     getEnv("NEO4J_PASSWORD", "password"),
		DataDir:       getEnv("DATA_DIR", "./documents"),
		OllamaHost:    getEnv("OLLAMA_HOST", "http://localhost:11434"),
		OpenAIAPIKey:  os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL: getEnv("OPENAI_BASE_URL", ""),
		Embeddings: EmbeddingConfig{
			Provider:  getEnv("EMBEDDING_PROVIDER", ProviderOllama),
			Model:     getEnv("EMBEDDING_MODEL", "nomic-embed-text"),
			Dimension: getEnvInt("EMBEDDING_DIMENSION", 768),
		},
		LLM: LLMConfig{
			Provider: getEnv("LLM_PROVIDER", ProviderOllama),
			Model:    getEnv("LLM_MODEL", "llama3.1:8b"),
		},
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
