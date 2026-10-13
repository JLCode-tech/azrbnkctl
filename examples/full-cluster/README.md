# Full Cluster Reference Deployment (`dual-interface`)

This directory provides the production-grade reference architecture for running **F5 BIG-IP Next for Kubernetes (BNK) 2.4** on **Azure Kubernetes Service (AKS)**.

| Attribute | Specification |
|---|---|
| **Cluster Name** | `bnk-full-cluster` |
| **Region** | `eastus2` |
| **Topology Pattern** | `dual-interface` (segregated external and internal data paths) |
| **System Node Pool** | 2 × `Standard_D4s_v5` (Control plane, FLO, CoreDNS) |
| **TMM Node Pool** | 1 × `Standard_D8s_v5` with **Accelerated Networking** |
| **Egress** | Azure NAT Gateway on `snet-public` (`10.0.16.0/28`) |
| **Estimated Cost** | ~$3.50 / hour |

---

## Network Architecture

```
  snet-system (10.0.0.0/20) ───────────► AKS System Nodes (Azure CNI Overlay)
  snet-public (10.0.16.0/28) ──────────► Azure Public NAT Gateway (Outbound Egress)
  
  snet-external (10.0.17.0/24) ────────► BNK External Data Path (net1 / 1.1)
  │                                      ├─ TMM External Self-IP (.224 - .254)
  │                                      └─ Listener VIPs (.100 - .150)
  │
  snet-internal (10.0.18.0/24) ────────► BNK Internal Data Path (net2 / 1.2)
                                         └─ TMM Internal Self-IP (East-West traffic)
```

---

## Deployment Steps

### 1. Configure Credentials
Place your F5 FAR auth key and subscription JWT into `keys/`:
```bash
mkdir -p examples/full-cluster/keys
cp /path/to/f5-far-auth-key.tgz examples/full-cluster/keys/
cp /path/to/license.jwt examples/full-cluster/keys/
```

### 2. Validate Configuration
```bash
./azrbnkctl validate examples/full-cluster/cluster.yaml
```

### 3. Dry-Run Plan
```bash
./azrbnkctl up -f examples/full-cluster/cluster.yaml --dry-run
```

### 4. Deploy Infrastructure & BNK
```bash
./azrbnkctl up -f examples/full-cluster/cluster.yaml
```

### 5. Run Automated Verification Scenarios
```bash
./azrbnkctl scenarios run --all -f examples/full-cluster/cluster.yaml
```

### 6. Teardown
```bash
./azrbnkctl down -f examples/full-cluster/cluster.yaml
```
