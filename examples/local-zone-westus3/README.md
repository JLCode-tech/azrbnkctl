# local-zone-westus3 — Phoenix Edge Zone Cluster

| Attribute | Specification |
|---|---|
| **Cluster Name** | `local-zone-westus3` |
| **Location** | `westus3` |
| **Data Plane** | Dual-interface Multi-Networking with Accelerated Networking |
| **VM Sizes** | 2 × `Standard_D4s_v5` (system), 1 × `Standard_D8s_v5` (TMM) |
| **Estimated Cost** | ~$3.00 / hour |

This blueprint configures an edge cluster topology for the `westus3` (Phoenix) Azure Edge Zone.

---

## Deployment & Verification

```bash
# Validate intent
./azrbnkctl validate examples/local-zone-westus3/cluster.yaml

# Provision
./azrbnkctl up -f examples/local-zone-westus3/cluster.yaml

# Run validation scenarios
./azrbnkctl scenarios run --all -f examples/local-zone-westus3/cluster.yaml

# Teardown
./azrbnkctl down -f examples/local-zone-westus3/cluster.yaml
```
