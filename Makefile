SHELL := /bin/sh
ENV_FILE ?= .env
TEST_PKGS := ./tests/...
INCLUDE_INTEGRATION ?= 0
BINARY := go-agent
CLEAR_ARGS ?=
SERVE_ARGS ?=

.PHONY: test run deps deps-go deps-ui build go-test clear serve lint infra-up infra-down infra-status ci-local help

## test: Install dependencies, lint, build, and run tests. Set INCLUDE_INTEGRATION=1 to enable integration checks.
test: deps lint build go-test
	@echo "Test pipeline complete"

## run: Install dependencies, start infrastructure, and run API + UI dev servers.
run: deps
	@$(MAKE) infra-up
	@echo "Starting development servers (Ctrl+C to stop)"
	@( cleanup() { \
	        echo ""; \
	        echo "Stopping development servers"; \
	        if [ -n "$$GO_PID" ]; then kill "$$GO_PID" 2>/dev/null || true; fi; \
	        if [ -n "$$UI_PID" ]; then kill "$$UI_PID" 2>/dev/null || true; fi; \
	        if [ -n "$$GO_PID$$UI_PID" ]; then wait "$$GO_PID" "$$UI_PID" 2>/dev/null || true; fi; \
	    }; \
	    GO_PID=""; \
	    UI_PID=""; \
	    trap cleanup INT TERM EXIT; \
	    (cd ui && npm run dev) & \
	    UI_PID=$$!; \
	    ( set -a; \
	      [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; \
	      set +a; \
	      go run . serve $(SERVE_ARGS) ) & \
	    GO_PID=$$!; \
	    wait "$$GO_PID" "$$UI_PID" )

## deps-go: Download Go module dependencies.
deps-go:
	@echo "Fetching Go dependencies"
	go mod download

## deps-ui: Install frontend dependencies.
deps-ui:
	@echo "Installing UI dependencies"
	cd ui && npm install

## deps: Install all project dependencies (Go + UI).
deps: deps-go deps-ui

## build: Download modules and build the go-agent binary.
build:
	@echo "Building $(BINARY)"
	@mkdir -p bin
	go build -o bin/$(BINARY) .

## go-test: Run Go tests under ./tests. Set INCLUDE_INTEGRATION=1 to enable integration checks.
go-test:
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
