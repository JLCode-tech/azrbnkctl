#!/usr/bin/env bash
# Local CI gate set - runs lint (gofmt, go vet, staticcheck), go test -race, and go build.
set -euo pipefail

cd "$(dirname "$0")/.."

echo "-> gofmt -d -l ."
out="$(gofmt -d -l .)"
if [ -n "$out" ]; then
	echo "$out"
	echo "gofmt: files need formatting (run 'gofmt -w .')"
	exit 1
fi

echo "-> go vet ./..."
go vet ./...

echo "-> go tool staticcheck ./..."
go tool staticcheck ./...

echo "-> go test -race ./..."
go test -race ./...

echo "-> go build ./..."
go build ./...

echo "[PASS] All local CI gates passed."
