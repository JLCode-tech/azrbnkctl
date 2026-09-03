.PHONY: build test vet tidy run clean

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BIN     := bin/azrbnkctl
PKG     := github.com/JLCode-tech/azrbnkctl

LDFLAGS := -s -w \
	-X $(PKG)/internal/cli.Version=$(VERSION) \
	-X $(PKG)/internal/cli.Commit=$(COMMIT) \
	-X $(PKG)/internal/cli.BuildDate=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/azrbnkctl

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

run: build
	$(BIN) --help

clean:
	rm -rf bin/ dist/

.PHONY: release stamp-changelog goreleaser-check goreleaser-snapshot staticcheck lint ci-local pre-commit-install

RELEASE_DATE ?= $(shell date +%Y-%m-%d)
GORELEASER_IMAGE ?= goreleaser/goreleaser:latest

stamp-changelog:
	@if grep -q '2026-MM-DD' CHANGELOG.md; then \
	    sed -i '' "s/2026-MM-DD/$(RELEASE_DATE)/" CHANGELOG.md 2>/dev/null || sed -i "s/2026-MM-DD/$(RELEASE_DATE)/" CHANGELOG.md; \
	    echo "    CHANGELOG.md date stamped: $(RELEASE_DATE)"; \
	else \
	    echo "    CHANGELOG.md date already stamped (skip)"; \
	fi

goreleaser-check:
	docker run --rm -v $(CURDIR):/work -w /work $(GORELEASER_IMAGE) check

goreleaser-snapshot:
	docker run --rm \
	    -v $(CURDIR):/work \
	    -w /work \
	    $(GORELEASER_IMAGE) release --snapshot --clean

staticcheck:
	go tool staticcheck ./...

lint:
	@out=$$(gofmt -d -l .); if [ -n "$$out" ]; then echo "$$out"; echo "gofmt: files need formatting (run 'gofmt -w .')"; exit 1; fi
	go vet ./...
	$(MAKE) staticcheck

ci-local: lint
	go test -race ./...
	go build ./...

pre-commit-install:
	ln -sf ../../scripts/pre-commit.sh .git/hooks/pre-commit && echo "Pre-commit hook installed."
