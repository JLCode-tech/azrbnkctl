#!/usr/bin/env bash
set -euo pipefail

echo "Running pre-commit checks for azrbnkctl..."

# 1. Gofmt check
out=$(gofmt -d -l .)
if [ -n "$out" ]; then
    echo "$out"
    echo "gofmt: files need formatting (run 'gofmt -w .')"
    exit 1
fi

# 2. Go vet
go vet ./...

# 3. Unit tests
go test ./...

echo "All pre-commit checks passed!"
