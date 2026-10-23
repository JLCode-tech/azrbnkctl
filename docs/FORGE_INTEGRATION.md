# BNK Forge Integration

`azrbnkctl` features built-in integration with **F5 BNK Forge** (`azrbnkctl forge`, with `azrbnkctl bnkforge` retained as a backward-compatibility alias) for real-time topology visualization, fleet telemetry, template catalog management, security/readiness scanning, and AI benchmark ingestion.

## 1. Overview & Transport Architecture

`azrbnkctl forge` uses a **dual-transport client** (`internal/forge` and `internal/bnkforge`):
- **MCP-First (`/mcp/`)**: Connects to BNK Forge's Streamable HTTP MCP endpoint (default `http://localhost:8081/mcp/`) to discover projects, register clusters, manage templates, and query live cluster state.
- **REST Fallback (`/api/...`)**: Automatically falls back to the Forge REST API (default `http://localhost:8000`) for agent token generation, telemetry streaming (`/api/telemetry`), and `aiperf` benchmark result ingestion (`/api/benchmarks/results`).

## 2. Declarative `cluster.yaml` Configuration

Enable automatic Forge registration during `azrbnkctl up` (Phase 29) and automatic unregistration during `azrbnkctl down` by adding a `forge:` block to `cluster.yaml`:

```yaml
forge:
  enabled: true
  mcpUrl: http://localhost:8081/mcp/
  projectName: azrbnkctl-bnk-agentfoundry-demo
  clusterName: bnk-agentfoundry-demo
```

## 3. Command Reference (`azrbnkctl forge`)

| Subcommand | Description |
| :--- | :--- |
| `azrbnkctl forge register -f cluster.yaml` | Register the AKS cluster with BNK Forge, deploy `bnk-forge-agent` in `f5-utils`, and write `.azrbnkctl/<cluster>/forge_link.json` |
| `azrbnkctl forge status -f cluster.yaml` | Show registration state, project/cluster IDs, and live MCP reachability |
| `azrbnkctl forge unregister -f cluster.yaml` | Unregister the cluster from BNK Forge and remove local link metadata |
| `azrbnkctl forge cleanup` | Prune stale or orphaned `azrbnkctl-*` projects and clusters in BNK Forge |
| `azrbnkctl forge template list` | List available BNK configuration templates in Forge |
| `azrbnkctl forge template create` | Publish a new BNK template to the Forge catalog |
| `azrbnkctl forge scan -f cluster.yaml` | Run BNK 2.4 readiness, CRD, and dataplane health scan and report findings |
| `azrbnkctl forge telemetry -f cluster.yaml` | Collect and stream live TMM governance, dataplane, and pod metrics to Forge |
| `azrbnkctl forge benchmark -f cluster.yaml` | Run `aiperf` from the Azure jumphost VM against the BNK VIP and push results to Forge |

## 4. Cluster Registration & Status

Registering an AKS cluster to Forge is performed automatically during `azrbnkctl up` (when `forge.enabled: true`) or standalone via:

```bash
azrbnkctl forge register -f cluster.yaml
```

### Registration Capabilities
- **Project Discovery**: Automatically detects or creates the project container in Forge.
- **Cluster Linking**: Registers the AKS API server endpoint, CA certificate, and token credentials.
- **Agent Token Generation**: Automatically injects the `bnk-forge-agent` into the `f5-utils` namespace on the AKS cluster.
- **Link Persistence**: Stores project and cluster IDs in `.azrbnkctl/<cluster>/forge_link.json`.

Check registration and reachability:
```bash
azrbnkctl forge status -f cluster.yaml
```

Example Output:
```text
BNK-Forge Status for workspace "bnk-agentfoundry-demo":
  Status:        registered
  Project:       azrbnkctl-bnk-agentfoundry-demo (id=11)
  Cluster:       bnk-agentfoundry-demo (id=11)
  Registered At: 2026-09-03 08:34:40 UTC
  Reachable:     true
```

## 5. Telemetry & Benchmarks

Stream governance and TMM dataplane telemetry to BNK Forge:
```bash
azrbnkctl forge telemetry -f examples/agentfoundry-demo/cluster.yaml \
  --forge-url http://localhost:8000 \
  --interval 15s
```

Run an `aiperf` LLM benchmark from the Azure jumphost VM (`JUMPHOST_PUBLIC_IP`) against the BNK Gateway VIP and upload structured latency/throughput metrics to Forge:
```bash
azrbnkctl forge benchmark \
  --region australiaeast \
  --instance-id jumphost-vm \
  --vip 10.0.17.100 \
  --model meta-llama/Llama-3.1-8B-Instruct \
  --forge-rest-url http://localhost:8000 \
  --scenarios latency,throughput
```
