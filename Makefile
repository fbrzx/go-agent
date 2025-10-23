SHELL := /bin/sh
ENV_FILE ?= .env
TEST_PKGS := ./tests/...
INCLUDE_INTEGRATION ?= 0
BINARY := go-agent
TRAIN_ARGS ?=
CHAT_ARGS ?=
CLEAR_ARGS ?=
SERVE_ARGS ?=

.PHONY: test build train chat clear serve lint infra-up infra-down infra-status ci-local help

## test: Run Go tests under ./tests. Set INCLUDE_INTEGRATION=1 to enable integration checks.
test:
	@if [ "$(INCLUDE_INTEGRATION)" = "1" ]; then \
		echo "Running tests in $(TEST_PKGS) (integration enabled)"; \
	else \
		echo "Running tests in $(TEST_PKGS) (integration skipped)"; \
	fi
	@( set -a; \
	   [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; \
	   set +a; \
	   if [ "$(INCLUDE_INTEGRATION)" = "1" ]; then \
	       RUN_DB_INTEGRATION_TESTS=1 go test $(TEST_PKGS); \
	   else \
	       go test $(TEST_PKGS); \
	   fi )

## build: Download modules and build the go-agent binary.
build:
	@echo "Tidying modules and building $(BINARY)"
	go mod tidy
	@mkdir -p bin
	go build -o bin/$(BINARY) .

## train: Execute the ingestion command, sourcing variables from $(ENV_FILE) if present.
ingest:
	@echo "Running $(BINARY) document ingestion"
	@( set -a; \
	   [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; \
	   set +a; \
	   go run . ingest $(TRAIN_ARGS) )

## chat: Execute the chat command, sourcing variables from $(ENV_FILE) if present.
chat:
	@echo "Running $(BINARY) chat"
	@( set -a; \
	   [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; \
	   set +a; \
	   go run . chat $(CHAT_ARGS) )

## clear: Remove ingested data from Postgres and Neo4j. Set CONFIRM=1 to skip the prompt.
clear:
	@echo "Clearing $(BINARY) data"
	@( set -a; \
	   [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; \
	   set +a; \
	   ARGS="$(CLEAR_ARGS)"; \
	   if [ "$(CONFIRM)" = "1" ]; then ARGS="--confirm $$ARGS"; fi; \
	   go run . clear $$ARGS )

## serve: Start the HTTP API that exposes train/chat/clear via OpenAPI.
serve:
	@echo "Starting $(BINARY) HTTP API"
	@( set -a; \
	   [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; \
	   set +a; \
	   go run . serve $(SERVE_ARGS) )

## lint: Run all linting checks (Go + Frontend).
lint:
	@./scripts/lint.sh

## infra-up: Start development infrastructure (PostgreSQL, Neo4j, Ollama).
infra-up:
	@./scripts/infra.sh up

## infra-down: Stop development infrastructure.
infra-down:
	@./scripts/infra.sh down

## infra-status: Check status of development infrastructure.
infra-status:
	@./scripts/infra.sh status

## ci-local: Run full CI pipeline locally.
ci-local:
	@./scripts/ci-local.sh

## help: Display this help message.
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/^## /  /'
