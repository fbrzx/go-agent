# Continuous Integration & Testing

This document describes the CI/CD pipeline, testing strategy, and local development workflows.

## Table of Contents

- [Overview](#overview)
- [GitHub Actions Pipeline](#github-actions-pipeline)
- [Local Development](#local-development)
- [Linting](#linting)
- [Testing](#testing)
- [Helper Scripts](#helper-scripts)
- [Troubleshooting](#troubleshooting)

## Overview

The project uses GitHub Actions for continuous integration with the following checks:

1. **Go Linting** - Code quality and style checks
2. **Frontend Linting** - TypeScript and formatting checks
3. **Backend Build** - Compile Go application
4. **Frontend Build** - Build React UI
5. **Unit Tests** - Fast tests without external dependencies
6. **Integration Tests** - Tests with PostgreSQL and Neo4j
7. **Docker Build** - Test container image creation
8. **Docker Compose** - Test full stack deployment
9. **Security Scanning** - Vulnerability detection
10. **Code Coverage** - Test coverage reporting

## GitHub Actions Pipeline

### Workflow File

Location: `.github/workflows/ci.yml`

### Triggers

- Push to `main` or `master` branch
- Pull requests to `main` or `master` branch

### Jobs Overview

```
┌─────────────────────────────────────────────────────────┐
│                    CI Pipeline                          │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  ┌──────────────┐  ┌──────────────┐                    │
│  │  Lint Go     │  │ Lint Frontend│                    │
│  └──────┬───────┘  └──────┬───────┘                    │
│         │                  │                            │
│         ▼                  ▼                            │
│  ┌──────────────┐  ┌──────────────┐                    │
│  │ Build Backend│  │Build Frontend│                    │
│  └──────┬───────┘  └──────┬───────┘                    │
│         │                  │                            │
│         └────────┬─────────┘                            │
│                  ▼                                      │
│         ┌────────────────┐                              │
│         │  Unit Tests    │                              │
│         └────────┬───────┘                              │
│                  │                                      │
│         ┌────────▼───────────┐                          │
│         │ Integration Tests  │                          │
│         └────────┬───────────┘                          │
│                  │                                      │
│         ┌────────▼────────┐                             │
│         │  Docker Build   │                             │
│         └────────┬────────┘                             │
│                  │                                      │
│         ┌────────▼─────────────┐                        │
│         │ Docker Compose Test  │                        │
│         └──────────────────────┘                        │
│                                                          │
│         ┌──────────────────────┐                        │
│         │   Security Scan      │                        │
│         └──────────────────────┘                        │
│                                                          │
└─────────────────────────────────────────────────────────┘
```

### Job Details

#### 1. Lint Go Code (`lint-go`)

**Purpose**: Ensure Go code quality and consistency

**Checks**:
- `golangci-lint` - Comprehensive linting (see `.golangci.yml`)
- `gofmt` - Code formatting
- `go vet` - Go toolchain validation

**Duration**: ~2 minutes

#### 2. Lint Frontend (`lint-frontend`)

**Purpose**: Ensure TypeScript code quality

**Checks**:
- TypeScript type checking (`tsc --noEmit`)
- Prettier formatting checks

**Duration**: ~1 minute

#### 3. Build Backend (`build-backend`)

**Purpose**: Verify Go application compiles

**Steps**:
- Download dependencies
- Verify `go.mod` and `go.sum`
- Build binary

**Artifacts**: `go-agent-binary` (7 days retention)

**Duration**: ~3 minutes

#### 4. Build Frontend (`build-frontend`)

**Purpose**: Verify React UI builds

**Steps**:
- Install npm dependencies
- Build with Vite
- Verify output files

**Artifacts**: `ui-dist` (7 days retention)

**Duration**: ~2 minutes

#### 5. Unit Tests (`unit-tests`)

**Purpose**: Run tests without external dependencies

**Features**:
- Race condition detection (`-race`)
- Code coverage collection
- Coverage upload to Codecov

**Artifacts**: `coverage-report` HTML (7 days retention)

**Duration**: ~2 minutes

#### 6. Integration Tests (`integration-tests`)

**Purpose**: Test with real databases

**Services**:
- PostgreSQL 16.4 with pgvector
- Neo4j 5.24 Community

**Features**:
- GitHub Actions services for databases
- Automatic health checks
- Coverage reporting

**Duration**: ~5 minutes

#### 7. Docker Build (`docker-build`)

**Purpose**: Test Docker image creation

**Features**:
- Multi-stage build test
- Build cache optimization
- Image verification

**Duration**: ~4 minutes

#### 8. Docker Compose Test (`docker-compose-test`)

**Purpose**: Test full stack deployment

**Verifies**:
- All services start correctly
- Health checks pass
- Connections work
- Clean teardown

**Duration**: ~3 minutes

#### 9. Security Scan (`security-scan`)

**Purpose**: Detect vulnerabilities

**Tools**:
- Trivy - Container and filesystem scanning
- gosec - Go security checker

**Artifacts**: Security reports (30 days retention)

**Duration**: ~3 minutes

#### 10. Code Quality Summary (`code-quality`)

**Purpose**: Generate summary report

**Output**: GitHub Actions summary page with results

**Duration**: <1 minute

### Total Pipeline Duration

- **Minimum**: ~20 minutes (all jobs in parallel)
- **Typical**: ~25 minutes

## Local Development

### Prerequisites

```bash
# Required
- Go 1.23+
- Node.js 20+
- Docker & Docker Compose
- Make

# Optional (installed by scripts if missing)
- golangci-lint
```

### Quick Start

```bash
# 1. Start infrastructure
./scripts/infra.sh up

# 2. Run all checks
./scripts/ci-local.sh

# 3. Stop infrastructure when done
./scripts/infra.sh down
```

## Linting

### Go Linting

**Configuration**: `.golangci.yml`

**Run locally**:
```bash
# All linters
golangci-lint run ./...

# Auto-fix issues
golangci-lint run --fix ./...

# Format code
gofmt -s -w .

# Check with go vet
go vet ./...
```

**Using script**:
```bash
./scripts/lint.sh
```

**Enabled Linters**:
- `errcheck` - Unchecked error detection
- `gosimple` - Code simplification
- `govet` - Go toolchain checks
- `ineffassign` - Ineffectual assignment detection
- `staticcheck` - Advanced static analysis
- `unused` - Unused code detection
- `gofmt` - Formatting
- `goimports` - Import formatting
- `misspell` - Spell checking
- `gocritic` - Opinionated checks
- `revive` - Fast, configurable linting
- `gosec` - Security issues
- `bodyclose` - HTTP body close checks
- `exportloopref` - Loop variable capture
- `noctx` - Missing context detection
- `sqlclosecheck` - SQL resource leaks

### Frontend Linting

**Configuration**:
- `ui/.prettierrc` - Prettier formatting
- `ui/tsconfig.json` - TypeScript configuration

**Run locally**:
```bash
cd ui

# Type checking
npm run typecheck

# Format check
npm run format:check

# Auto-format
npm run format
```

## Testing

### Test Structure

```
tests/
├── unit/              # Unit tests (no external deps)
│   ├── llm_test.go
│   ├── embeddings_test.go
│   ├── ingestion_test.go
│   ├── chat_service_test.go
│   └── ...
└── integration/       # Integration tests (require DBs)
    ├── db_connection_test.go
    ├── vector_search_test.go
    └── graph_insights_test.go
```

### Running Tests

#### Unit Tests Only

```bash
# Simple
go test ./tests/unit/...

# With coverage
go test -v -race -coverprofile=coverage.txt ./tests/unit/...

# Using script
./scripts/test.sh
```

#### With Integration Tests

```bash
# Start infrastructure first
./scripts/infra.sh up

# Run all tests
INCLUDE_INTEGRATION=1 ./scripts/test.sh

# Or use Makefile
make test INCLUDE_INTEGRATION=1
```

#### Test a Specific Package

```bash
# Single package
go test -v ./chat/...

# Specific test
go test -v -run TestSpecificFunction ./chat/...
```

### Code Coverage

**View coverage**:
```bash
# Terminal summary
go test -coverprofile=coverage.txt ./...
go tool cover -func=coverage.txt

# HTML report
go tool cover -html=coverage.txt -o coverage.html
open coverage.html  # macOS
xdg-open coverage.html  # Linux
```

**Coverage uploaded to Codecov** on CI runs (requires token).

### Writing Tests

#### Unit Test Example

```go
// tests/unit/example_test.go
package unit

import (
    "testing"
)

func TestExample(t *testing.T) {
    result := YourFunction()
    if result != expected {
        t.Errorf("got %v, want %v", result, expected)
    }
}
```

#### Integration Test Example

```go
// tests/integration/example_test.go
package integration

import (
    "os"
    "testing"
)

func TestWithDatabase(t *testing.T) {
    if os.Getenv("RUN_DB_INTEGRATION_TESTS") != "1" {
        t.Skip("Skipping integration test")
    }

    // Test with real database...
}
```

## Helper Scripts

### `scripts/infra.sh`

Manage development infrastructure.

```bash
# Start services
./scripts/infra.sh up

# Check status
./scripts/infra.sh status

# View logs
./scripts/infra.sh logs

# Stop services
./scripts/infra.sh down

# Clean (removes data)
./scripts/infra.sh clean
```

**Services managed**:
- PostgreSQL (port 5432)
- Neo4j (ports 7474, 7687)
- Ollama (port 11434)

### `scripts/lint.sh`

Run all linting checks.

```bash
./scripts/lint.sh
```

**Checks**:
- Go: golangci-lint, gofmt, go vet
- Frontend: TypeScript, Prettier

### `scripts/test.sh`

Run test suite.

```bash
# Unit tests only
./scripts/test.sh

# With integration tests
INCLUDE_INTEGRATION=1 ./scripts/test.sh
```

**Features**:
- Automatically checks if services are running
- Generates coverage reports
- Clear error messages

### `scripts/ci-local.sh`

Run full CI pipeline locally.

```bash
./scripts/ci-local.sh
```

**Steps**:
1. Linting (Go + Frontend)
2. Build backend
3. Build frontend
4. Unit tests
5. Integration tests (if services available)
6. Docker build
7. Generate reports

**Duration**: ~5-10 minutes

## Troubleshooting

### Linting Failures

**Problem**: `golangci-lint` not found

```bash
# Install golangci-lint
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
  sh -s -- -b $(go env GOPATH)/bin v1.61.0

# Add to PATH
export PATH=$PATH:$(go env GOPATH)/bin
```

**Problem**: Formatting errors

```bash
# Auto-fix formatting
gofmt -s -w .
cd ui && npm run format
```

**Problem**: Too many linting errors

```bash
# Fix auto-fixable issues
golangci-lint run --fix ./...

# Check specific linter
golangci-lint run --disable-all --enable=errcheck ./...
```

### Test Failures

**Problem**: Integration tests fail with connection errors

```bash
# Check if services are running
./scripts/infra.sh status

# Restart services
./scripts/infra.sh down
./scripts/infra.sh up
```

**Problem**: Tests timeout

```bash
# Increase timeout
go test -timeout 5m ./...
```

**Problem**: Race condition detected

```bash
# Run without race detector to isolate issue
go test ./...

# Then fix the specific test
go test -race -run TestSpecificFunction ./...
```

### Docker Issues

**Problem**: Docker build fails

```bash
# Clean Docker cache
docker builder prune -af

# Rebuild from scratch
docker build --no-cache -t go-agent .
```

**Problem**: Services won't start

```bash
# Check logs
docker compose logs postgres
docker compose logs neo4j

# Clean and restart
./scripts/infra.sh clean
./scripts/infra.sh up
```

**Problem**: Port conflicts

```bash
# Check what's using the port
lsof -i :5432
lsof -i :7687
lsof -i :7474

# Kill conflicting process or change ports in docker-compose.dev.yml
```

### CI Pipeline Issues

**Problem**: CI passes locally but fails on GitHub

- Check Go/Node versions match (`.github/workflows/ci.yml`)
- Verify all dependencies are in `go.mod` / `package.json`
- Check for hardcoded paths

**Problem**: Flaky tests

- Add retries for network operations
- Increase timeouts in CI environment
- Use `t.Parallel()` carefully
- Ensure proper cleanup in `defer` statements

**Problem**: Slow CI pipeline

- Optimize tests (mock external services)
- Use build caching effectively
- Run independent tests in parallel
- Consider splitting into multiple workflows

## Best Practices

### Writing Tests

1. **Use table-driven tests**:
```go
tests := []struct {
    name     string
    input    string
    expected string
}{
    {"case1", "input1", "output1"},
    {"case2", "input2", "output2"},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test logic
    })
}
```

2. **Proper cleanup**:
```go
func TestExample(t *testing.T) {
    // Setup
    resource := setup()
    defer resource.Cleanup()

    // Test logic
}
```

3. **Use testify for assertions** (optional):
```go
import "github.com/stretchr/testify/assert"

assert.Equal(t, expected, actual)
assert.NoError(t, err)
```

### Code Review

Before submitting PR:

```bash
# Run full CI locally
./scripts/ci-local.sh

# Or step by step
./scripts/lint.sh
./scripts/test.sh
docker build -t go-agent:test .
```

### Commit Messages

Follow conventional commits:
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation
- `test:` - Tests
- `ci:` - CI/CD changes
- `refactor:` - Code refactoring
- `perf:` - Performance improvements

## Additional Resources

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [golangci-lint Configuration](https://golangci-lint.run/usage/configuration/)
- [Go Testing Package](https://pkg.go.dev/testing)
- [Codecov Documentation](https://docs.codecov.com/)
- [Trivy Documentation](https://aquasecurity.github.io/trivy/)

## Getting Help

If you encounter issues:

1. Check this document's troubleshooting section
2. Run `./scripts/infra.sh status` to verify services
3. Check CI logs on GitHub Actions
4. Review recent commits that passed CI
5. Open an issue with:
   - Command you ran
   - Full error output
   - Output of `./scripts/infra.sh status`
   - Go version: `go version`
   - Docker version: `docker --version`
