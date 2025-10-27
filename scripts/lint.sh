#!/bin/bash
set -e

# Script to run all linting checks locally

echo "=================================="
echo "Running Go Linting Checks"
echo "=================================="

# Check if golangci-lint is installed
if ! command -v golangci-lint &> /dev/null; then
    echo "Installing golangci-lint..."
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.64.6
    export PATH=$PATH:$(go env GOPATH)/bin
fi

echo ""
echo "1. Running golangci-lint..."
PACKAGE_DIRS=$(go list -f '{{.Dir}}' ./...)
golangci-lint run --modules-download-mode=mod --timeout 5m ${PACKAGE_DIRS}

echo ""
echo "2. Checking Go formatting..."
UNFORMATTED=$(gofmt -s -l . | grep -v vendor || true)
if [ -n "$UNFORMATTED" ]; then
    echo "❌ The following files are not formatted correctly:"
    echo "$UNFORMATTED"
    echo ""
    echo "Run 'gofmt -s -w .' to fix"
    exit 1
fi
echo "✅ All Go files are formatted correctly"

echo ""
echo "3. Running go vet..."
go vet ./...
echo "✅ go vet passed"

echo ""
echo "=================================="
echo "Running Frontend Linting Checks"
echo "=================================="

cd ui

if [ ! -d "node_modules" ]; then
    echo "Installing frontend dependencies..."
    npm ci
fi

echo ""
echo "1. Checking TypeScript types..."
npm run typecheck
echo "✅ TypeScript check passed"

echo ""
echo "2. Checking code formatting..."
npm run format:check
echo "✅ Prettier check passed"

cd ..

echo ""
echo "=================================="
echo "✅ All linting checks passed!"
echo "=================================="
