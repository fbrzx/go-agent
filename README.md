# go-agent

Agentic RAG scaffold for experimenting with Markdown knowledge bases. The project ingests `.md` files, chunks and embeds their contents into Postgres (with pgvector), and mirrors the knowledge structure inside Neo4j. Embedding generation is pluggable so you can point the pipeline at local Ollama models or hosted OpenAI APIs.

## Quick Start with Docker (Recommended)

The easiest way to get started is using Docker Compose, which sets up all dependencies automatically:

```bash
# 1. Copy environment configuration
cp .env.docker .env

# 2. Start all services (PostgreSQL, Neo4j, Ollama, and the app)
docker compose up -d

# 3. Wait for initialization (5-10 minutes for first run)
docker compose logs -f ollama-setup

# 4. Access the application
# Web UI: http://localhost:8080
# API Docs: http://localhost:8080/openapi.yaml
# Neo4j Browser: http://localhost:7474
```

See [DOCKER.md](DOCKER.md) for comprehensive Docker deployment documentation.

## Prerequisites (Manual Setup)

- Go 1.20+
- PostgreSQL 15+ with the `vector` extension (pgvector)
- Neo4j 5.x
- Optional but default: [Ollama](https://ollama.com) running locally with the `llama3.1:8b` and `nomic-embed-text` models pulled
- Optional: OpenAI API access when using hosted models

## Configuration

All runtime settings can be stored in `.env` (already git-ignored). Defaults favour a fully local stack.

| Variable | Default | Purpose |
| --- | --- | --- |
| `POSTGRES_DSN` | `postgres://localhost:5432/go-agent?sslmode=disable` | Postgres connection string |
| `NEO4J_URI` | `neo4j://localhost:7687` | Neo4j Bolt endpoint |
| `NEO4J_USERNAME` | `neo4j` | Neo4j username |
| `NEO4J_PASSWORD` | `password` | Neo4j password |
| `DATA_DIR` | `./documents` | Where Markdown sources live |
| `OLLAMA_HOST` | `http://localhost:11434` | Ollama HTTP endpoint |
| `LLM_PROVIDER` | `ollama` (`ollama`\|`openai`) | Conversational model provider |
| `LLM_MODEL` | `llama3.1:8b` | Chat/agent model name |
| `EMBEDDING_PROVIDER` | `ollama` (`ollama`\|`openai`) | Embedding provider |
| `EMBEDDING_MODEL` | `nomic-embed-text` | Embedding model name |
| `EMBEDDING_DIMENSION` | `768` | Vector dimension to store in pgvector |
| `OPENAI_API_KEY` | _unset_ | Required when `*_PROVIDER=openai` |
| `OPENAI_BASE_URL` | _unset_ | Override for Azure/OpenAI-compatible endpoints |

Update `.env` and export the file before building or testing:

```sh
set -a
. ./.env
set +a
```

## Usage

1. Install Go dependencies and compile the binary:
   ```sh
   make build
   ```
2. Place Markdown documents in the directory configured by `DATA_DIR` (default `./documents`).
3. Run the ingestion pipeline:
   ```sh
   make train
   ```
   Add `TRAIN_ARGS="--dir ./other/path"` to ingest a different folder.
4. Ask the agent a question over the indexed knowledge base:
   ```sh
   make chat CHAT_ARGS="--question 'What is our adoption strategy?'"
   ```
   Omit `--question` to open an interactive session where replies stream token-by-token and each follow-up keeps the full conversation context. Use `--limit` to adjust how many chunks feed the answer. Add repeated `--topics` or `--sections` flags to constrain the response context:
   ```sh
   make chat CHAT_ARGS="--question 'Summarise adoption' --topics adoption --topics onboarding --sections introduction"
   ```
5. Clear previously ingested data (requires confirmation):
   ```sh
   make clear
   ```
   Run `make clear CONFIRM=1` to skip the confirmation prompt.

Behind the scenes the command:
- Ensures the `vector` extension and RAG tables exist in Postgres.
- Splits Markdown content into overlapping chunks tailored to the format.
- Generates embeddings through the configured provider.
- Stores vectors inside `rag_chunks` and mirrors document/chunk relationships in Neo4j.
- Captures folder hierarchy and cross-document relationships inside Neo4j for richer retrieval context.
- Tracks section hierarchy and shared topics so chunk embeddings stay aligned with both Postgres metadata and Neo4j relationships, enabling topic-similarity navigation during chat.

## Development Tasks

- `make ingest` – run the CLI with optional `TRAIN_ARGS` overrides (e.g., `--dir`).
- `make chat` – query the agent; combine with `CHAT_ARGS="--question '...'"`.
- `make clear` – wipe Postgres tables and Neo4j graph (`CONFIRM=1` to bypass the prompt).
- `make test` – run unit tests (set `INCLUDE_INTEGRATION=1` to exercise live DB connectivity).
- `make build` – refresh modules and build `bin/go-agent`.
- `make serve` – launch the HTTP API that mirrors `ingest`, `chat`, and `clear` via OpenAPI.

## HTTP API

Run `make serve` (or `go run . serve --addr :9090`) to start the JSON API. It exposes the same
workflows as the CLI (existing `make` targets continue to run the local commands directly):

- `POST /v1/ingest` – trigger ingestion (optional body `{ "dir": "./other/docs" }`).
- `POST /v1/chat` – ask a question with body `{ "question": "...", "limit": 5 }` and optional section/topic filters.
- `POST /v1/chat/stream` – identical contract but streams `text/event-stream` chunks for real-time output.
- `POST /v1/clear` – clear persisted data; requires `{ "confirm": true }`.
- `GET /healthz` – lightweight readiness probe.
- `GET /openapi.yaml` – download the full OpenAPI 3.0 contract.

Responses include detailed error payloads and source metadata identical to the CLI output. See
`api/openapi.yaml` for the authoritative schema.

## Web UI

Run `make serve` and open `http://localhost:8080/` to try the new React-powered dashboard. Multi-file
uploads (including drag & drop anywhere in the chat panel) are available out of the box, and each
successful ingest surfaces chunk counts inline. The chat composer streams responses token-by-token
and preserves full conversation history across turns.

### Frontend workflow

The UI lives under `ui/` and is built with Vite + React + TypeScript. Development and build scripts:

```sh
cd ui
npm install       # once
npm run dev       # hot-reload development server
npm run build     # emits assets into ../api/ui/dist for Go embedding
```

`npm run dev` starts Vite on port 5173; use it during frontend development and point the Go API at the
same backend (`make serve`) for live requests. `npm run build` must run before shipping a release so the
embedded assets stay in sync with the Go binary.

## Project Layout

- `config/` – environment-driven configuration and defaults.
- `database/` – connection helpers plus schema bootstrapping for pgvector tables.
- `embeddings/` – pluggable clients for Ollama and OpenAI embeddings.
- `llm/` – language-model clients matching the same provider choices.
- `chat/` – retrieval augmented chat orchestration tying vectors, graph insights, and LLM completions together.
- `ingestion/` – document chunking logic and persistence into Postgres/Neo4j.
- `knowledge/` – Neo4j graph synchronisation helpers.
- `tests/integration/` – opt-in connectivity tests for Postgres and Neo4j.

Extend this scaffold with retrieval/query handlers, agent loops, or additional knowledge graph relationships as your RAG workflows evolve.

## Docker Deployment

### Production Deployment

The application includes a complete Docker Compose setup with all dependencies:

```bash
# Start everything
docker compose up -d

# View logs
docker compose logs -f

# Stop services
docker compose down
```

**Included Services:**
- **go-agent**: Main application with optimized multi-stage build
- **postgres**: PostgreSQL 16 with pgvector extension
- **neo4j**: Neo4j 5.24 Community Edition with APOC plugins
- **ollama**: Local LLM inference engine (optional, can use OpenAI instead)

### Development with Docker

For local development, use the dev compose file to run only infrastructure:

```bash
# Start databases only
docker compose -f docker-compose.dev.yml up -d

# Run the Go app locally
make serve
```

### Configuration

Edit `.env` to configure services:
- Switch between Ollama (local) and OpenAI (cloud) providers
- Adjust database passwords and connection strings
- Configure LLM and embedding models

### Performance Optimizations

The Docker setup includes several optimizations:
- **Multi-stage builds**: Minimal final image size (~50MB for Go binary)
- **Connection pooling**: Reused database connections across requests
- **Health checks**: Automatic service dependency management
- **Resource limits**: Tuned memory and CPU allocations
- **Persistent volumes**: Data survives container restarts

For detailed Docker documentation including troubleshooting, monitoring, and production best practices, see [DOCKER.md](DOCKER.md).

## Continuous Integration

The project includes a comprehensive CI/CD pipeline with GitHub Actions:

- **Automated Testing**: Unit and integration tests on every PR
- **Code Quality**: Linting for Go and TypeScript
- **Security Scanning**: Vulnerability detection with Trivy and gosec
- **Docker Validation**: Automated build and compose stack testing
- **Code Coverage**: Automatic coverage reporting

### Local CI Checks

Run the full CI pipeline locally before pushing:

```bash
# Run complete CI suite
./scripts/ci-local.sh

# Or run individual checks
./scripts/lint.sh           # Linting only
./scripts/test.sh           # Unit tests
INCLUDE_INTEGRATION=1 \
  ./scripts/test.sh         # With integration tests
```

### Helper Scripts

```bash
# Manage development infrastructure
./scripts/infra.sh up       # Start PostgreSQL, Neo4j, Ollama
./scripts/infra.sh status   # Check service health
./scripts/infra.sh down     # Stop services
./scripts/infra.sh clean    # Remove all data

# Run tests
./scripts/test.sh           # Unit tests
INCLUDE_INTEGRATION=1 \
  ./scripts/test.sh         # All tests

# Linting
./scripts/lint.sh           # Check code quality
```

For comprehensive CI/CD documentation, see [CI.md](CI.md).
