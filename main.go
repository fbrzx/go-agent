package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/database"
	"github.com/fabfab/go-agent/embeddings"
	"github.com/fabfab/go-agent/ingestion"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags)

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cfg := config.Load()

	switch os.Args[1] {
	case "ingest":
		ingestCmd(cfg, logger, os.Args[2:])
	default:
		logger.Printf("unknown command: %s", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func ingestCmd(cfg config.Config, logger *log.Logger, args []string) {
	flags := flag.NewFlagSet("ingest", flag.ExitOnError)
	dataDir := flags.String("dir", cfg.DataDir, "path to directory containing markdown documents")
	if err := flags.Parse(args); err != nil {
		logger.Fatalf("parse ingest flags: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pgPool, err := database.NewPostgresPool(ctx, cfg.PostgresDSN)
	if err != nil {
		logger.Fatalf("postgres connection: %v", err)
	}
	defer pgPool.Close()

	neo4jDriver, err := database.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPass)
	if err != nil {
		logger.Fatalf("neo4j connection: %v", err)
	}
	defer neo4jDriver.Close(ctx)

	embedder, err := embeddings.NewEmbedder(cfg)
	if err != nil {
		logger.Fatalf("embedder setup: %v", err)
	}

	svc := ingestion.NewService(pgPool, neo4jDriver, embedder, logger, cfg.Embeddings.Dimension)
	logger.Printf("ingesting markdown from %s using %s/%s embeddings", *dataDir, strings.ToUpper(cfg.Embeddings.Provider), cfg.Embeddings.Model)

	if err := svc.IngestDirectory(ctx, *dataDir); err != nil {
		logger.Fatalf("ingestion failed: %v", err)
	}
}

func printUsage() {
	fmt.Println("Usage: go-agent <command> [options]")
	fmt.Println("Commands:")
	fmt.Println("  ingest   Ingest markdown documents into Postgres/Neo4j (use --dir to override data directory)")
}
