SHELL := /bin/sh
ENV_FILE ?= .env
TEST_PKGS := ./tests/...
INCLUDE_INTEGRATION ?= 0
BINARY := go-agent

.PHONY: test build run

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
	go build -o bin/$(BINARY) .

## run: Execute the application, sourcing variables from $(ENV_FILE) if present.
run:
	@echo "Running $(BINARY)"
	@( set -a; \
	   [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; \
	   set +a; \
	   go run . )
