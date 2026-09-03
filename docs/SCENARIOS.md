# Validation Scenarios & Day-2 Operations

`azrbnkctl` provides built-in validation scenarios and Day-2 testing tools for testing live AKS deployments of F5 BIG-IP Next for Kubernetes.

## 1. Built-in Testing Commands

### DNS Probe & Latency Distribution
Probe DNS resolution against upstream or internal resolvers and compute percentiles:

```bash
azrbnkctl test dns --target f5.com --server 8.8.8.8:53 --iterations 5
```

### TCP/UDP Connectivity Reachability
Probe network endpoints with round-trip latency measurement:

```bash
azrbnkctl test connectivity --target 10.0.17.10:443 --proto tcp --count 3
```

### Egress Forwarding & SNAT Verification
Validate Phase 50 egress configuration on the deployed cluster:

```bash
azrbnkctl test egress -f cluster.yaml
```

## 2. Example Scenarios

- **External Only**: Single external interface deployment for ingress edge traffic.
- **Dual Interface**: North-south external and east-west internal network interfaces with secondary Accelerated Networking VFs.
- **AgentFoundry Demo**: Dual-interface high-performance AI Gateway deployment with Redis caching and telemetry export.
