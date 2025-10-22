package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// NewPostgresPool creates a new PostgreSQL connection pool with optimized settings
func NewPostgresPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres DSN: %w", err)
	}

	// Optimize connection pool settings
	config.MaxConns = 25                       // Maximum number of connections in the pool
	config.MinConns = 5                        // Minimum number of connections to maintain
	config.MaxConnLifetime = 1 * time.Hour     // Maximum lifetime of a connection
	config.MaxConnIdleTime = 30 * time.Minute  // Maximum time a connection can be idle
	config.HealthCheckPeriod = 1 * time.Minute // How often to check connection health

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

// NewNeo4jDriver creates a new Neo4j driver with optimized settings
func NewNeo4jDriver(ctx context.Context, uri, user, password string) (neo4j.DriverWithContext, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(user, password, ""), func(config *neo4j.Config) {
		config.MaxConnectionPoolSize = 50
		config.MaxConnectionLifetime = 1 * time.Hour
		config.ConnectionAcquisitionTimeout = 60 * time.Second
		config.SocketConnectTimeout = 10 * time.Second
		config.SocketKeepalive = true
	})
	if err != nil {
		return nil, fmt.Errorf("create neo4j driver: %w", err)
	}

	// Verify connectivity
	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(ctx)
		return nil, fmt.Errorf("verify neo4j connectivity: %w", err)
	}

	return driver, nil
}
