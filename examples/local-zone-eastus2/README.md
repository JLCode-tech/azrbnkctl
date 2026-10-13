# local-zone-eastus2 — Azure Local / Edge Cluster

| Attribute | Specification |
|---|---|
| **Cluster Name** | `local-zone-eastus2` |
| **Location** | `eastus2` |
| **Data Plane** | Dual-interface Multi-Networking with Accelerated Networking |
| **VM Sizes** | 2 × `Standard_D4s_v5` (system), 1 × `Standard_D8s_v5` (TMM) |
| **Estimated Cost** | ~$3.00 / hour |

This example matches the verified production deployment documented in [`docs/AZURE_AKS_BNK24_DEPLOYMENT_REPORT.md`](../../docs/AZURE_AKS_BNK24_DEPLOYMENT_REPORT.md).

---

## Deployment & Verification

```bash
# Validate intent
./azrbnkctl validate examples/local-zone-eastus2/cluster.yaml

# Provision
./azrbnkctl up -f examples/local-zone-eastus2/cluster.yaml

# Deploy HTTP/2 edge scenario
kubectl apply -f examples/local-zone-eastus2/manifests/http2.yaml

# Verify data path
kubectl apply -f examples/local-zone-eastus2/curl-test.yaml
kubectl logs -f job/curl-test
```
