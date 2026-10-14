# azrbnkctl Phase Lifecycle Execution Graph

`azrbnkctl` deploys Azure infrastructure and BNK software across 21 sequential phases:

```
[Phase 00: Preflight]
       │
       ▼
[Phase 10: Resource Group] ──► [Phase 11: NSG] ──► [Phase 12: VNet & Subnets]
                                                            │
       ┌────────────────────────────────────────────────────┘
       ▼
[Phase 13: NAT Gateway] ──► [Phase 14: Ingress PIP]
       │
       ▼
[Phase 21: AKS Cluster] ──► [Phase 22: TMM Node Pool] ──► [Phase 23: Kubeconfig]
                                                                  │
       ┌──────────────────────────────────────────────────────────┘
       ▼
[Phase 24: Node Labels] ──► [Phase 25: Dataplane Discovery]
       │
       ▼
[Phase 30: Namespaces & FAR Secret] ──► [Phase 31: cert-manager]
                                                │
       ┌────────────────────────────────────────┘
       ▼
[Phase 32: FLO Operator] ──► [Phase 33: CWC Certs & Issuers] ──► [Phase 34: CNEInstance CR]
                                                                        │
       ┌────────────────────────────────────────────────────────────────┘
       ▼
[Phase 35: License CR] ──► [Phase 36: Gateway API Class] ──► [Phase 37: VLANs & Routes]
                                                                    │
       ┌────────────────────────────────────────────────────────────┘
       ▼
[Phase 40: BNK TMM Activation Wait] ──► [Phase 50: Egress & NAT Verification]
```

---

## Detailed Phase Breakdown

| Phase | Identifier | Purpose |
| :--- | :--- | :--- |
| **00** | `preflight` | Verify Azure credentials, subscription access, quota, and location availability. |
| **10** | `resource-group` | Create or adopt the Azure Resource Group (`azrbnkctl-<name>-rg`). |
| **11** | `nsg` | Create Network Security Group (`nsg-<name>`) with default allow rules. |
| **12** | `vnet` | Create VNet (`10.0.0.0/16`) and subnets (`snet-system`, `snet-public`, `snet-external`, `snet-internal`). |
| **13** | `nat-gateway` | Create NAT Gateway (`natgw-<name>`) and associate with system subnet. |
| **14** | `public-ips` | Allocate ingress Public IP (`pip-ingress-<name>`) for BNK VIP listeners. |
| **21** | `aks-cluster` | Provision AKS Managed Cluster (`aks-<name>`) with system node pool (Azure CNI Overlay). |
| **22** | `tmm-nodepool` | Provision TMM node pool (`Standard_D8s_v5`) with Accelerated Networking enabled. |
| **23** | `kubeconfig` | Fetch AKS cluster admin credentials and write to `.azrbnkctl/<name>/kubeconfig`. |
| **24** | `node-labels` | Verify TMM node labels (`app=f5-tmm`) for workload scheduling. |
| **25** | `discovery` | Discover external/internal PCI addresses and compute default gateway endpoints. |
| **30** | `namespaces` | Create `f5-bnk`, `f5-utils`, `egress-demo` namespaces and apply `f5-far-secret`. |
| **31** | `cert-manager` | Apply cert-manager v1.16.1 CRDs and controllers via dynamic SSA. |
| **32** | `flo` | Apply F5 Lifecycle Orchestrator (FLO) deployment manifests and CRDs. |
| **33** | `cwc-certs` | Apply ClusterIssuers and verify `ext-ca` certificate issuance. |
| **34** | `cneinstance` | Render and apply `CNEInstance` custom resource with discovered PCI addresses. |
| **35** | `license` | Render and apply F5 BNK License CR from JWT secret. |
| **36** | `gateway-api` | Apply GatewayClass (`<cluster-name>-gatewayclass`) to enable Kubernetes Gateway API routing. |
| **37** | `vlans-routes` | Render and apply F5SPKVlan CRs and static dataplane routes. |
| **40** | `activation` | Wait for CNEInstance readiness, FLO reconciliation, and TMM pod status. |
| **50** | `egress` | Configure and verify egress test client forwarding through F5 TMM. |

---

## Teardown Sequence (`azrbnkctl down`)

Resource teardown runs in strict reverse dependency order to avoid cloud locking:
1. **AKS Managed Cluster**: Delete AKS cluster and worker node pools.
2. **NAT Gateway & Public IPs**: Release NAT Gateway and associated public IP addresses.
3. **Virtual Networks & NSG**: Delete VNet, subnets, and Network Security Group.
4. **Resource Group**: Preserve Resource Group by default to protect shared RBAC and policy scopes. Deleted only if `--delete-rg` is explicitly specified.

