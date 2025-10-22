#!/bin/bash
set -e

# Script to run all tests locally

INCLUDE_INTEGRATION=${INCLUDE_INTEGRATION:-0}

echo "=================================="
echo "Running Test Suite"
echo "=================================="

echo ""
echo "1. Running unit tests..."
go test -v -race -coverprofile=coverage.txt -covermode=atomic ./tests/unit/...
echo "✅ Unit tests passed"

if [ "$INCLUDE_INTEGRATION" = "1" ]; then
    echo ""
    echo "2. Running integration tests..."

    # Check if required services are running
    if ! pg_isready -h localhost -p 5432 > /dev/null 2>&1; then
        echo "❌ PostgreSQL is not running on localhost:5432"
        echo "Start it with: docker compose -f docker-compose.dev.yml up -d postgres"
        exit 1
    fi

    if ! curl -s http://localhost:7474 > /dev/null 2>&1; then
        echo "❌ Neo4j is not running on localhost:7474"
        echo "Start it with: docker compose -f docker-compose.dev.yml up -d neo4j"
        exit 1
    fi

    export RUN_DB_INTEGRATION_TESTS=1
    go test -v -race -coverprofile=integration-coverage.txt -covermode=atomic ./tests/integration/...
    echo "✅ Integration tests passed"
else
    echo ""
    echo "ℹ️  Skipping integration tests (set INCLUDE_INTEGRATION=1 to run them)"
    echo "   Make sure services are running: docker compose -f docker-compose.dev.yml up -d"
fi

echo ""
echo "=================================="
echo "Generating coverage report..."
echo "=================================="

if [ -f coverage.txt ]; then
    go tool cover -func=coverage.txt | tail -n 1
    go tool cover -html=coverage.txt -o coverage.html
    echo "✅ Coverage report generated: coverage.html"
fi

echo ""
echo "=================================="
echo "✅ All tests passed!"
echo "=================================="
