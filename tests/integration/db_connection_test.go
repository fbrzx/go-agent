package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/fabfab/go-agent/config"
	"github.com/fabfab/go-agent/database"
)

func TestDatabaseConnectivity(t *testing.T) {
	if os.Getenv("RUN_DB_INTEGRATION_TESTS") != "1" {
		t.Skip("set RUN_DB_INTEGRATION_TESTS=1 to run database connectivity checks")
	}

	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pgPool, err := database.NewPostgresPool(ctx, cfg.PostgresDSN)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}
	defer pgPool.Close()

	if err := pgPool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping postgres: %v", err)
	}

	driver, err := database.NewNeo4jDriver(ctx, cfg.Neo4jURI, cfg.Neo4jUser, cfg.Neo4jPass)
	if err != nil {
		t.Fatalf("failed to create neo4j driver: %v", err)
	}
	defer func() {
		if closeErr := driver.Close(ctx); closeErr != nil {
			t.Errorf("failed to close neo4j driver: %v", closeErr)
		}
	}()

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer func() {
		if closeErr := session.Close(ctx); closeErr != nil {
			t.Errorf("failed to close neo4j session: %v", closeErr)
		}
	}()

	result, err := session.Run(ctx, "RETURN 1 AS ok", nil)
	if err != nil {
		t.Fatalf("failed to run neo4j ping query: %v", err)
	}

	ok := result.Next(ctx)
	if !ok {
		if err := result.Err(); err != nil {
			t.Fatalf("neo4j query error: %v", err)
		}
		t.Fatal("neo4j query returned no records")
	}

	value, found := result.Record().Get("ok")
	if !found {
		t.Fatal("neo4j query missing 'ok' field")
	}

	intValue, ok := value.(int64)
	if !ok || intValue != 1 {
		t.Fatalf("unexpected neo4j return value: %#v", value)
	}

	if err := result.Err(); err != nil {
		t.Fatalf("neo4j result error: %v", err)
	}
}
