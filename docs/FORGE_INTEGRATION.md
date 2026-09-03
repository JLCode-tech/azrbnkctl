# BNK Forge Integration

`azrbnkctl` features built-in integration with **F5 BNK Forge** for real-time visualization, telemetry, and automated project catalog management.

## 1. Overview

BNK Forge provides centralized cluster lifecycle discovery, TMM dataplane metrics aggregation, and AI assistant connectivity via the Model Context Protocol (MCP).

## 2. Cluster Registration

Registering an AKS cluster to Forge is performed during `azrbnkctl up` or standalone via:

```bash
azrbnkctl forge register -f cluster.yaml
```

### Registration Capabilities
- **Project Discovery**: Automatically detects or creates the project container in Forge.
- **Cluster Linking**: Registers the AKS API server endpoint, CA certificate, and token credentials.
- **Agent Token Generation**: Automatically injects the `bnk-forge-agent` into the `f5-utils` namespace on the AKS cluster.
- **Link Persistence**: Stores project and cluster IDs in `.azrbnkctl/<cluster>/forge-link.json`.

## 3. Viewing Cluster Status in Forge

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

## 4. MCP Agent Integration

`azrbnkctl` can communicate directly with Forge's embedded MCP server (`/mcp/`) to query cluster health, list workloads, and trigger automated diagnostics.
