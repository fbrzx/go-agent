package config

import "os"

type Config struct {
	PostgresDSN string
	Neo4jURI    string
	Neo4jUser   string
	Neo4jPass   string
}

func Load() Config {
	return Config{
		PostgresDSN: getEnv("POSTGRES_DSN", "postgres://localhost:5432/go-agent?sslmode=disable"),
		Neo4jURI:    getEnv("NEO4J_URI", "neo4j://localhost:7687"),
		Neo4jUser:   getEnv("NEO4J_USERNAME", "neo4j"),
		Neo4jPass:   getEnv("NEO4J_PASSWORD", "password"),
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
