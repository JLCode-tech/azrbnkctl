#!/usr/bin/env bash
# Local CI gate set — mirrors .github/workflows/ci.yml (11 gates).
set -euo pipefail

cd "$(dirname "$0")/.."

echo "== [1/11] gofmt -d -l . =="
out="$(gofmt -d -l .)"
if [ -n "$out" ]; then
	echo "$out"
	echo "gofmt: files need formatting (run 'gofmt -w .')"
	exit 1
fi

echo "== [2/11] go vet ./... =="
go vet ./...

echo "== [3/11] go tool staticcheck ./... =="
go tool staticcheck ./...

echo "== [4/11] go test -race ./... =="
go test -race ./...

echo "== [5/11] ./scripts/lint-shell.sh + go build (host + windows/amd64) =="
./scripts/lint-shell.sh
mkdir -p bin
go build -o bin/azrbnkctl ./cmd/azrbnkctl
GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/azrbnkctl

echo "== [6/11] azrbnkctl test {connectivity,dns,egress} --dry-run =="
./bin/azrbnkctl test connectivity --target 10.0.17.10:443 --dry-run >/dev/null
./bin/azrbnkctl test dns --target f5.com --dry-run >/dev/null
./bin/azrbnkctl test egress -f examples/dual-interface/cluster.yaml --dry-run >/dev/null

echo "== [7/11] azrbnkctl validate + up/down --dry-run =="
./bin/azrbnkctl validate examples/dual-interface/cluster.yaml
./bin/azrbnkctl validate examples/external-only/cluster.yaml
./bin/azrbnkctl validate examples/agentfoundry-demo/cluster.yaml
AZRBNKCTL_SKIP_AUTH=1 ./bin/azrbnkctl up -f examples/dual-interface/cluster.yaml --dry-run >/dev/null
AZRBNKCTL_SKIP_AUTH=1 ./bin/azrbnkctl down -f examples/dual-interface/cluster.yaml --dry-run >/dev/null
AZRBNKCTL_SKIP_AUTH=1 ./bin/azrbnkctl up -f examples/external-only/cluster.yaml --dry-run >/dev/null
AZRBNKCTL_SKIP_AUTH=1 ./bin/azrbnkctl down -f examples/external-only/cluster.yaml --dry-run >/dev/null

echo "== [8/11] goreleaser check =="
if command -v goreleaser >/dev/null 2>&1; then
	goreleaser check
else
	echo "  (goreleaser not on PATH; skipping)"
fi

echo "== [9/11] cspell (docs/**/*.md + README.md + CONTRIBUTING.md) =="
if command -v cspell >/dev/null 2>&1; then
	cspell --config cspell.json "docs/**/*.md" README.md CONTRIBUTING.md
elif command -v npx >/dev/null 2>&1; then
	npx --yes cspell --config cspell.json "docs/**/*.md" README.md CONTRIBUTING.md
else
	echo "  (cspell/npx not on PATH; skipping)"
fi

echo "== [10/11] gosec ./... =="
if command -v gosec >/dev/null 2>&1; then
	gosec -quiet ./...
elif [ -x "$(go env GOPATH)/bin/gosec" ]; then
	"$(go env GOPATH)/bin/gosec" -quiet ./...
else
	echo "  (gosec not installed; skipping)"
fi

echo "== [11/11] govulncheck-gate.sh + gitleaks =="
if command -v govulncheck >/dev/null 2>&1 || [ -x "$(go env GOPATH)/bin/govulncheck" ]; then
	./scripts/govulncheck-gate.sh
else
	echo "  (govulncheck not installed; skipping)"
fi
if command -v gitleaks >/dev/null 2>&1; then
	gitleaks detect --source . --no-banner --redact -v
else
	echo "  (gitleaks not installed; skipping)"
fi

echo "[PASS] All local CI gates passed."
