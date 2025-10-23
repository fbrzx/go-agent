#!/bin/bash
set -e

# Script to run the full CI pipeline locally

echo "╔═══════════════════════════════════════════════╗"
echo "║  Running Full CI Pipeline Locally            ║"
echo "╚═══════════════════════════════════════════════╝"

# Step 1: Linting
echo ""
echo "📋 Step 1: Linting"
echo "-------------------"
./scripts/lint.sh

# Step 2: Build backend
echo ""
echo "🔨 Step 2: Building Backend"
echo "----------------------------"
go mod download
go mod verify
mkdir -p bin
go build -v -o bin/go-agent .
echo "✅ Backend built successfully"

# Step 3: Build frontend
echo ""
echo "🎨 Step 3: Building Frontend"
echo "-----------------------------"
cd ui
if [ ! -d "node_modules" ]; then
    npm ci
fi
npm run build
cd ..

if [ ! -f "api/ui/dist/index.html" ]; then
    echo "❌ Frontend build failed"
    exit 1
fi
echo "✅ Frontend built successfully"

# Step 4: Unit tests
echo ""
echo "🧪 Step 4: Running Unit Tests"
echo "------------------------------"
go test -v -race -coverprofile=coverage.txt -covermode=atomic ./tests/unit/...
echo "✅ Unit tests passed"

# Step 5: Integration tests (if services are running)
echo ""
echo "🔗 Step 5: Integration Tests"
echo "-----------------------------"
if pg_isready -h localhost -p 5432 > /dev/null 2>&1 && curl -s http://localhost:7474 > /dev/null 2>&1; then
    echo "Services detected, running integration tests..."
    export RUN_DB_INTEGRATION_TESTS=1
    go test -v -race -coverprofile=integration-coverage.txt -covermode=atomic ./tests/integration/...
    echo "✅ Integration tests passed"
else
    echo "⚠️  Services not running, skipping integration tests"
    echo "   To run integration tests, start services with:"
    echo "   docker compose -f docker-compose.dev.yml up -d"
fi

# Step 6: Docker build
echo ""
echo "🐳 Step 6: Testing Docker Build"
echo "--------------------------------"
docker build -t go-agent:ci-test .
echo "✅ Docker image built successfully"

# Cleanup
docker rmi go-agent:ci-test > /dev/null 2>&1 || true

# Final summary
echo ""
echo "╔═══════════════════════════════════════════════╗"
echo "║  ✅ All CI checks passed!                     ║"
echo "╚═══════════════════════════════════════════════╝"
echo ""
echo "Coverage reports:"
if [ -f coverage.txt ]; then
    echo "  - Unit tests: coverage.txt"
    go tool cover -func=coverage.txt | tail -n 1
fi
if [ -f integration-coverage.txt ]; then
    echo "  - Integration tests: integration-coverage.txt"
    go tool cover -func=integration-coverage.txt | tail -n 1
fi
echo ""
echo "Ready to push! 🚀"
