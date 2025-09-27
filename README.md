# go-agent

Agentic RAG scaffold for experimenting with Markdown knowledge bases. The project ingests `.md` files, chunks and embeds their contents into Postgres (with pgvector), and mirrors the knowledge structure inside Neo4j. Embedding generation is pluggable so you can point the pipeline at local Ollama models or hosted OpenAI APIs.

## Prerequisites

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
   make ingest
   ```
   Add `INGEST_ARGS="--dir ./other/path"` to ingest a different folder.
4. Ask the agent a question over the indexed knowledge base:
   ```sh
   make run CHAT_ARGS="--question 'What is our adoption strategy?'"
   ```
   Omit `--question` to be prompted interactively. Use `--limit` to adjust how many chunks feed the answer.
5. Clear previously ingested data (requires confirmation):
   ```sh
   make clear
   ```
   Run `make clear CONFIRM=1` to skip the confirmation prompt.

Behind the scenes the command:
- Ensures the `vector` extension and RAG tables exist in Postgres.
- Splits Markdown into overlapping chunks.
- Generates embeddings through the configured provider.
- Stores vectors inside `rag_chunks` and mirrors document/chunk relationships in Neo4j.

## Development Tasks

- `make ingest` – run the CLI with optional `INGEST_ARGS` overrides (e.g., `--dir`).
- `make run` – query the agent; combine with `CHAT_ARGS="--question '...'"`.
- `make clear` – wipe Postgres tables and Neo4j graph (`CONFIRM=1` to bypass the prompt).
- `make test` – run unit tests (set `INCLUDE_INTEGRATION=1` to exercise live DB connectivity).
- `make build` – refresh modules and build `bin/go-agent`.

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
