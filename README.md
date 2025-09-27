# go-agent

A minimal Go application augmented with PostgreSQL + pgvector and Neo4j clients. The program loads connection settings, attempts to initialise both drivers, and logs any connectivity issues.

## Prerequisites

- Go 1.20 or later
- (Optional) Running PostgreSQL with the pgvector extension enabled
- (Optional) Running Neo4j 5.x

## Configuration

Connection settings are supplied via environment variables and fall back to useful local defaults:

| Variable | Default | Purpose |
| --- | --- | --- |
| `POSTGRES_DSN` | `postgres://localhost:5432/go-agent?sslmode=disable` | PostgreSQL connection string |
| `NEO4J_URI` | `neo4j://localhost:7687` | Neo4j bolt URI |
| `NEO4J_USERNAME` | `neo4j` | Neo4j username |
| `NEO4J_PASSWORD` | `password` | Neo4j password |

Override these before running the application if you have non-default credentials:

```sh
export POSTGRES_DSN="postgres://user:pass@db-host:5432/yourdb?sslmode=require"
export NEO4J_URI="neo4j+s://graph-host:7687"
export NEO4J_USERNAME="graph"
export NEO4J_PASSWORD="super-secret"
```

## Getting Started

1. Ensure modules are in sync:
   ```sh
   go mod tidy
   ```
2. Run the application:
   ```sh
   go run .
   ```
   If the databases are not reachable, the program logs a warning and continues.

## Project Layout

- `main.go` – entry point that loads config and wires up the database clients.
- `config/config.go` – lightweight configuration loader for database settings.
- `database/connections.go` – helpers for creating PostgreSQL pools and Neo4j drivers.
- `go.mod` / `go.sum` – module definition and dependencies.

Extend this skeleton with your own repositories, commands, and tests as needed.
