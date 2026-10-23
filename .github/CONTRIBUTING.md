# Contributing to azrbnkctl

Thank you for contributing to `azrbnkctl`!

## Development Workflow

### Prerequisites
- Go 1.25+ or Go 1.26
- Azure CLI (`az login`) or Service Principal environment variables (`AZURE_SUBSCRIPTION_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`)
- Docker (for GoReleaser config checks & snapshot builds)
- `staticcheck` (`go tool staticcheck ./...` via `go.mod` tool directive)
- Optional local gate tools: `goreleaser`, `cspell` (or `npx`), `gosec`, `govulncheck`

### Build & Test Locally

```bash
# Build local binary
make build

# Run unit tests with race detector
go test -race ./...

# Run full 11-gate local CI suite (mirrors .github/workflows/ci.yml)
make ci-local
# or directly:
./scripts/ci-local.sh
```

### Offline & Dry-Run Verification (Zero Cloud Credentials)

All provisioning, teardown, and test commands support deterministic `--dry-run` execution without live Azure credentials:

```bash
./bin/azrbnkctl validate examples/dual-interface/cluster.yaml
./bin/azrbnkctl validate examples/external-only/cluster.yaml
./bin/azrbnkctl validate examples/agentfoundry-demo/cluster.yaml

AZRBNKCTL_SKIP_AUTH=1 ./bin/azrbnkctl up -f examples/dual-interface/cluster.yaml --dry-run
AZRBNKCTL_SKIP_AUTH=1 ./bin/azrbnkctl down -f examples/dual-interface/cluster.yaml --dry-run
```

### Live Azure AKS End-to-End Verification

When testing against a live Azure subscription:
1. Authenticate via `az login` and export `AZURE_SUBSCRIPTION_ID=<subscription-uuid>`.
2. Run preflight diagnostics: `azrbnkctl doctor --subscription-id "$AZURE_SUBSCRIPTION_ID"`.
3. Provision a dedicated test cluster: `azrbnkctl up -f examples/agentfoundry-demo/cluster.yaml`.
4. Verify scenarios and dataplane health: `azrbnkctl bnk status -f examples/agentfoundry-demo/cluster.yaml` and `azrbnkctl scenarios clean --all -f examples/agentfoundry-demo/cluster.yaml`.
5. Tear down test resources: `azrbnkctl down -f examples/agentfoundry-demo/cluster.yaml --yes` (never modify or delete shared resource groups outside your test cluster).

## Branching & Pull Requests

1. All feature work and bug fixes should be branched off `staging`.
2. Open Pull Requests targeting `staging`.
3. Once validated on `staging`, promotions to `main` are made via PR for tag releases.

## Commit Message Conventions

This repository follows Conventional Commits:
- `feat:` new feature or CLI capability
- `fix:` bug fix
- `docs:` documentation updates
- `test:` test suite updates
- `chore:` maintenance, dependency bumps, or toolchain changes
