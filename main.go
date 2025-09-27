package main

import (
	"context"
	"log"

	"github.com/pgvector/pgvector-go"

	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/database"
)

func main() {
	log.Println("Welcome to go-agent!")

	cfg := config.Load()
	ctx := context.Background()

	pgPool, err := database.NewPostgresPool(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Printf("Postgres connection unavailable: %v", err)
	} else {
		defer pgPool.Close()
		vector := pgvector.NewVector([]float32{0.1, 0.2, 0.3})
		log.Printf("Sample vector ready for insertion: %v", vector.Slice())
	}

	neo4jDriver, err := database.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPass)
	if err != nil {
		log.Printf("Neo4j connection unavailable: %v", err)
		return
	}
	defer neo4jDriver.Close(ctx)

	log.Printf("Neo4j driver initialised for %s", cfg.Neo4jURI)
}
