package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/fabfab/go-agent/api"
	"github.com/fabfab/go-agent/chat"
	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/database"
	"github.com/fabfab/go-agent/embeddings"
	"github.com/fabfab/go-agent/ingestion"
	"github.com/fabfab/go-agent/llm"
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
	case "chat":
		chatCmd(cfg, logger, os.Args[2:])
	case "clear":
		clearCmd(cfg, logger, os.Args[2:])
	case "serve":
		serveCmd(cfg, logger, os.Args[2:])
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

func chatCmd(cfg config.Config, logger *log.Logger, args []string) {
	flags := flag.NewFlagSet("chat", flag.ExitOnError)
	question := flags.String("question", "", "question to ask the agent")
	limit := flags.Int("limit", 5, "number of context chunks to retrieve")
	sectionFilters := multiFlag{}
	topicFilters := multiFlag{}
	flags.Var(&sectionFilters, "sections", "section filter (repeatable)")
	flags.Var(&topicFilters, "topics", "topic filter (repeatable)")
	if err := flags.Parse(args); err != nil {
		logger.Fatalf("parse chat flags: %v", err)
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

	llmClient, err := llm.NewClient(cfg)
	if err != nil {
		logger.Fatalf("llm setup: %v", err)
	}

	vectorStore := chat.NewPostgresVectorStore(pgPool)
	graphStore := chat.NewNeo4jGraphStore(neo4jDriver)
	svc := chat.NewService(vectorStore, graphStore, embedder, llmClient, logger)

	conversationHistory := make([]llm.Message, 0)
	config := chat.Config{
		SimilarityLimit: *limit,
		SectionFilters:  sectionFilters.values,
		TopicFilters:    topicFilters.values,
	}

	scanner := bufio.NewScanner(os.Stdin)
	inputPending := strings.TrimSpace(*question)
	initialProvided := inputPending != ""
	if inputPending == "" {
		fmt.Println("Enter your question (type 'exit' to quit):")
	}

	for {
		if inputPending == "" {
			fmt.Print("You: ")
			if !scanner.Scan() {
				if err := scanner.Err(); err != nil {
					logger.Fatalf("read question: %v", err)
				}
				fmt.Println()
				return
			}
			inputPending = strings.TrimSpace(scanner.Text())
		}

		if strings.EqualFold(inputPending, "exit") || strings.EqualFold(inputPending, "quit") {
			fmt.Println("Bye!")
			return
		}
		if inputPending == "" {
			continue
		}
		if initialProvided {
			fmt.Printf("You: %s\n", inputPending)
			initialProvided = false
		}

		fmt.Print("Agent: ")
		resp, updatedHistory, err := svc.ChatStream(ctx, inputPending, config, conversationHistory, func(chunk string) error {
			fmt.Print(chunk)
			return nil
		})
		fmt.Println()
		if err != nil {
			logger.Printf("chat failed: %v", err)
			inputPending = ""
			continue
		}

		conversationHistory = updatedHistory

		if len(resp.Sources) > 0 {
			fmt.Println()
			if len(sectionFilters.values) > 0 {
				fmt.Printf("Filters (sections): %s\n", strings.Join(sectionFilters.values, ", "))
			}
			if len(topicFilters.values) > 0 {
				fmt.Printf("Filters (topics): %s\n", strings.Join(topicFilters.values, ", "))
			}
			fmt.Println("Sources:")
			for idx, source := range resp.Sources {
				fmt.Printf("%d. %s (%s)\n", idx+1, source.Title, source.Path)
				if source.Insight.ChunkCount > 0 {
					fmt.Printf("   Indexed chunks: %d\n", source.Insight.ChunkCount)
				}
				if len(source.Insight.Folders) > 0 {
					fmt.Printf("   Folders: %s\n", strings.Join(source.Insight.Folders, ", "))
				}
				if len(source.Insight.Sections) > 0 {
					sectionParts := make([]string, 0, len(source.Insight.Sections))
					for _, section := range source.Insight.Sections {
						if section.Title == "" {
							continue
						}
						sectionParts = append(sectionParts, fmt.Sprintf("%s (level %d)", section.Title, section.Level))
					}
					if len(sectionParts) > 0 {
						fmt.Printf("   Sections: %s\n", strings.Join(sectionParts, "; "))
					}
				}
				if len(source.Insight.Topics) > 0 {
					fmt.Printf("   Topics: %s\n", strings.Join(source.Insight.Topics, ", "))
				}
				if len(source.Insight.RelatedDocuments) > 0 {
					fmt.Println("   Related documents:")
					for _, related := range source.Insight.RelatedDocuments {
						extra := ""
						if related.Weight > 0 {
							extra += fmt.Sprintf(" weight %.2f", related.Weight)
						}
						if related.Similarity > 0 {
							extra += fmt.Sprintf(" sim %.2f", related.Similarity)
						}
						if related.Reason != "" {
							extra += fmt.Sprintf(" via %s", related.Reason)
						}
						fmt.Printf("     - %s (%s)%s\n", related.Title, related.Path, extra)
					}
				}
			}
		}

		fmt.Println()
		inputPending = ""
	}
}

func clearCmd(cfg config.Config, logger *log.Logger, args []string) {
	flags := flag.NewFlagSet("clear", flag.ExitOnError)
	confirmed := flags.Bool("confirm", false, "skip confirmation prompt")
	if err := flags.Parse(args); err != nil {
		logger.Fatalf("parse clear flags: %v", err)
	}

	if !*confirmed {
		fmt.Print("This will permanently delete ingested RAG data from Postgres and Neo4j. Continue? [y/N]: ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				logger.Fatalf("read confirmation: %v", err)
			}
			logger.Println("clear aborted")
			return
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if answer != "y" && answer != "yes" {
			logger.Println("clear aborted")
			return
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pgPool, err := database.NewPostgresPool(ctx, cfg.PostgresDSN)
	if err != nil {
		logger.Fatalf("postgres connection: %v", err)
	}
	defer pgPool.Close()

	if err := database.EnsureRAGSchema(ctx, pgPool, cfg.Embeddings.Dimension); err != nil {
		logger.Fatalf("ensure postgres schema: %v", err)
	}

	if _, err := pgPool.Exec(ctx, "TRUNCATE rag_chunks, rag_documents"); err != nil {
		logger.Fatalf("truncate postgres tables: %v", err)
	}
	logger.Println("cleared Postgres rag_documents and rag_chunks")

	neo4jDriver, err := database.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPass)
	if err != nil {
		logger.Fatalf("neo4j connection: %v", err)
	}
	defer neo4jDriver.Close(ctx)

	session := neo4jDriver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	if err := purgeNeo4j(ctx, session); err != nil {
		logger.Fatalf("clear neo4j: %v", err)
	}

	logger.Println("Neo4j documents and chunks cleared")
	logger.Println("RAG data removed")
}

func serveCmd(cfg config.Config, logger *log.Logger, args []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := flags.String("addr", ":8080", "address to bind the HTTP API server")
	if err := flags.Parse(args); err != nil {
		logger.Fatalf("parse serve flags: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	server, cleanup, err := api.New(cfg, logger)
	if err != nil {
		logger.Fatalf("initialize server: %v", err)
	}
	defer cleanup()

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Printf("HTTP API listening on %s", *addr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Printf("graceful shutdown failed: %v", err)
		}
		<-errCh
		logger.Println("HTTP API stopped")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Fatalf("http server error: %v", err)
		}
	}
}

func printUsage() {
	fmt.Println("Usage: go-agent <command> [options]")
	fmt.Println("Commands:")
	fmt.Println("  ingest   Ingest markdown documents into Postgres/Neo4j (use --dir to override data directory)")
	fmt.Println("  chat     Query the agent using the ingested knowledge base")
	fmt.Println("  clear    Remove ingested data from Postgres/Neo4j")
	fmt.Println("  serve    Start the HTTP API exposing ingest/chat/clear")
}

type multiFlag struct {
	values []string
}

func (m *multiFlag) String() string {
	if m == nil {
		return ""
	}
	return strings.Join(m.values, ",")
}

func (m *multiFlag) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	m.values = append(m.values, trimmed)
	return nil
}

func purgeNeo4j(ctx context.Context, session neo4j.SessionWithContext) error {
	queries := []string{
		"MATCH (d:Document) DETACH DELETE d",
		"MATCH (c:Chunk) DETACH DELETE c",
		"MATCH (f:Folder) DETACH DELETE f",
	}

	for _, query := range queries {
		result, err := session.Run(ctx, query, nil)
		if err != nil {
			return err
		}
		if _, err := result.Consume(ctx); err != nil {
			return err
		}
	}
	return nil
}
