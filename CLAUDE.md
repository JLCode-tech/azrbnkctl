# CLAUDE.md for azrbnkctl

## Overview
`azrbnkctl` is the Azure AKS member of the unified `*bnkctl` tool family (`awsbnkctl`, `gkebnkctl`, `roksbnkctl`, `ocibnkctl`). It automates F5 BIG-IP Next for Kubernetes (BNK) deployments on Azure AKS with Accelerated Networking, multi-subnet networking, pure ARM SDK v6 integration, Dynamic Server-Side Apply `client-go`, and complete tool-family CLI parity.

## Codebase Architecture
- `cmd/azrbnkctl/main.go`: CLI entrypoint.
- `internal/intent`: Declarative YAML schema loader, validation rules (k8s >= 1.32.0, VM size validation, subnet containment), and derived topological metadata.
- `internal/azure`: ARM SDK v6 client abstractions and `DefaultAzureCredential` auth with `AZRBNKCTL_SKIP_AUTH=1` support.
- `internal/azure/state`: Key-value state persistence in `.azrbnkctl/<cluster-name>/state.env`.
- `internal/azure/phases`: 21-phase topological lifecycle engine (`phase00_preflight` to `phase50_egress`) and teardown graph (`down.go`).
- `internal/k8s`: In-process Dynamic Server-Side Apply engine, manifest embedder, template renderer, and waiter.
- `internal/cli`: Cobra subcommands (`up`, `down`, `init`, `validate`, `doctor`, `kubeconfig`, `k`, `test`, `manifest`, `journal`, `agent`, `bnkforge`, `self`, `version`).
- `internal/doctor`: Preflight health checks for Azure CLI, kubectl, credentials, and Azure Resource Providers (`Microsoft.ContainerService`, `Microsoft.Network`, `Microsoft.Compute`).
- `internal/test`: DNS latency distribution probe (P50/P95/P99) and TCP/UDP reachability engine.
- `internal/journal`: Append-only JSONL audit trail logger.
- `internal/manifest`: F5 BNK release manifest inspector and offline fallback.
- `internal/kubeconfig`: AKS admin credentials fetcher and safe global config synchronization.

## Build and Test Commands
- **Build binary**: `make build`
- **Run unit tests**: `make test`
- **Run race detector**: `make test-race`
- **Lint / format check**: `make lint`
- **Full pre-push verification**: `make ci-local`

## Development Conventions
- **Pure Go**: No external CLI subprocesses for Azure operations. Use the Azure ARM SDK v6 (`armcontainerservice`, `armnetwork`, `armresources`).
- **Dynamic SSA**: Always use `internal/k8s` Server-Side Apply with FieldManager `azrbnkctl` and `Force: true`. Never shell out to `kubectl apply`.
- **Dry-Run Protection**: Every phase must respect `dryRun bool` and verify state without making remote Azure mutations.
- **Strict Error Handling**: Wrap all errors with contextual package/phase prefixes (e.g. `fmt.Errorf("phase21: creating aks: %w", err)`).
