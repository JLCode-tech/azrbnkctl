# Contributing to azrbnkctl

Thank you for contributing to `azrbnkctl`!

## Development Workflow

### Prerequisites
- Go 1.25+ or Go 1.26
- Azure CLI (`az`) with authenticated credentials
- Docker (for local linting & snapshot builds)
- `staticcheck` (`go install honnef.co/go/tools/cmd/staticcheck@latest`)

### Build & Test Locally

```bash
# Build local binary
make build

# Run unit tests
make test

# Run full local pre-push lint and test suite
make ci-local
```

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
