# azrbnkctl Phase Lifecycle Execution Graph

`azrbnkctl up` deploys Azure infrastructure, AKS node pools, BNK 2.4 software, and self-healing overlays across **27 sequential phases**:

```
[Phase 00: Preflight]
       │
       ▼
[Phase 10: Resource Group] ──► [Phase 11: NSG] ──► [Phase 12: VNet & Subnets]
                                                            │
       ┌────────────────────────────────────────────────────┘
       ▼
[Phase 13: NAT Gateway] ──► [Phase 14: Public IPs]
       │
       ▼
[Phase 21: AKS Cluster] ──► [Phase 22: TMM Node Pool] ──► [Phase 23: Kubeconfig]
                                                                  │
       ┌──────────────────────────────────────────────────────────┘
       ▼
[Phase 24: Node Labels] ──► [Phase 25: Dataplane Discovery] ──► [Phase 29: Forge Register]
                                                                          │
       ┌──────────────────────────────────────────────────────────────────┘
       ▼
[Phase 30: Namespaces & FAR Secret] ──► [Phase 31: cert-manager] ──► [Phase 31b: metrics-server]
                                                                              │
       ┌──────────────────────────────────────────────────────────────────────┘
       ▼
[Phase 33: CWC Certs & Issuers] ──► [Phase 32: FLO Operator] ──► [Phase 34: CNEInstance CR]
                                                                          │
       ┌──────────────────────────────────────────────────────────────────┘
       ▼
[Phase 35: License CR] ──► [Phase 36: Gateway API & BNK CRDs] ──► [Phase 37: Infra / VLANs & Routes]
                                                                          │
       ┌──────────────────────────────────────────────────────────────────┘
       ▼
[Phase 38: CWC Heal] ──► [Phase 38b: DSSM Overlay] ──► [Phase 38c: Pod-Manager Heal] ──► [Phase 38d: TMM Log Stream]
                                                                                                  │
       ┌──────────────────────────────────────────────────────────────────────────────────────────┘
       ▼
[Phase 40: BNK TMM Activation Wait] ──► [Phase 50: EgressGateway & SNAT]
```

---

## Detailed Phase Breakdown (27 `up` Phases)

| # | Phase | Identifier | Purpose |
| :--- | :--- | :--- | :--- |
| 1 | **00** | `preflight` | Verify Azure credentials, subscription access, quota, and location availability. |
| 2 | **10** | `resource-group` | Create or adopt the Azure Resource Group (`azrbnkctl-<name>-rg`). |
| 3 | **11** | `nsg` | Create Network Security Group (`nsg-<name>`) with default allow rules. |
| 4 | **12** | `vnet` | Create VNet (`10.0.0.0/16`) and subnets (`snet-system`, `snet-public`, `snet-external`, `snet-internal`). |
| 5 | **13** | `nat-gateway` | Create NAT Gateway (`natgw-<name>`) and associate with subnets. |
| 6 | **14** | `public-ips` | Allocate ingress Public IP (`pip-ingress-<name>`) for BNK VIP listeners. |
| 7 | **21** | `aks-cluster` | Provision AKS Managed Cluster (`aks-<name>`) with system node pool (Azure CNI Overlay, service CIDR `10.240.0.0/16`). |
| 8 | **22** | `tmm-nodepool` | Provision TMM node pool (`Standard_D8s_v5`) with Accelerated Networking (SR-IOV) and HugePages enabled. |
| 9 | **23** | `kubeconfig` | Fetch AKS cluster admin credentials and write to `.azrbnkctl/<name>/kubeconfig`. |
| 10 | **24** | `node-labels` | Apply and verify TMM node labels (`app=f5-tmm`) and Multus / SR-IOV daemonsets. |
| 11 | **25** | `discovery` | Discover external/internal SR-IOV PCI addresses (`TMM_EXT_PCI`, `TMM_INT_PCI`) and compute subnet Self-IPs and gateways. |
| 12 | **29** | `forge-register` | Optionally register the AKS cluster with [BNK Forge](https://github.com/JLCode-tech/bnk-forge) (`forge.enabled: true`) and deploy `bnk-forge-agent` in `f5-utils`. |
| 13 | **30** | `namespaces` | Create `cert-manager`, `f5-cne-core`, `f5-cne-system`, `f5-bnk-instance`, `f5-utils`, `egress-demo` namespaces, distribute `far-secret`, and configure `coredns-custom`. |
| 14 | **31** | `cert-manager` | Apply cert-manager v1.16.1 CRDs and controllers via dynamic Server-Side Apply. |
| 15 | **31b** | `metrics-server` | Ensure `metrics.k8s.io/v1beta1` API is served for `kubectl top` and BNK Forge fleet metrics. |
| 16 | **33** | `cwc-certs` | Generate and apply ClusterIssuers (`ext-ca`) and CWC TLS certificates prior to FLO startup. |
| 17 | **32** | `flo` | Install F5 Lifecycle Orchestrator (FLO `v2.30.0-0.5.2`) via embedded Helm SDK and apply controller EndpointSlices RBAC. |
| 18 | **34** | `cneinstance` | Render and apply `CNEInstance` CR (`manifestVersion: "2.4.0"`, `deploymentSize: "small"`, discovered PCI addresses, `TMM_K8S_ROUTES`). |
| 19 | **35** | `license` | Render and apply F5 BNK License secret / CR from operator JWT. |
| 20 | **36** | `gateway-api` | Apply Gateway API (`v1.2.1`), GIE (`v1.2.0`), and BNK 2.4 (`gateway.k8s.f5.com/v1alpha1`) CRDs, plus `f5-gateway-class` and `<name>-gatewayclass`. |
| 21 | **37** | `vlans-routes` | Render and apply BNK 2.4 `Infra` CR (`L2Network`, `L3Network`, Self-IPs, default route, listener pool `10.0.17.100–199`), or legacy BNK 2.3 `F5SPKVlan`/`F5SPKStaticRoute`. |
| 22 | **38** | `cwc-heal` | Detect and heal CWC pod DNS warm-up crash loops. |
| 23 | **38b** | `dssm-overlay` | Patch DSSM readiness probe scripts (`redis-cli --tls --insecure`) if DSSM pods are stalled on TLS hostname verification. |
| 24 | **38c** | `pod-manager-heal` | Verify and heal TMM `pod-manager` sidecar readiness. |
| 25 | **38d** | `tmm-log-stream` | Enable stdout store in `f5-cne-system/f5-toda-fluentd-custom-conf` so `azrbnkctl logs tmm` streams live TMM and governance logs. |
| 26 | **40** | `activation` | Wait for `CNEInstance` readiness, FLO reconciliation, and TMM pod readiness. |
| 27 | **50** | `egress` | Render and apply BNK 2.4 `GatewaySettings` + `EgressGateway` CRs in `bnk-demo` (or legacy BNK 2.3 `F5SPKEgress`) for dual-interface egress SNAT. |

---

## Teardown Sequence (`azrbnkctl down`)

Resource teardown runs in strict reverse dependency order to avoid cloud locking:
1. **BNK Forge Unregistration (`Phase29ForgeRegisterDown`)**: Unregisters the cluster from BNK Forge if previously linked.
2. **AKS Managed Cluster (`PhaseDownCluster`)**: Deletes the AKS cluster, worker node pools, and TMM secondary NICs.
3. **NAT Gateway & Public IPs (`PhaseDownNAT`)**: Disassociates and deletes the NAT Gateway and public IP addresses.
4. **Virtual Networks & NSG (`PhaseDownNetworks`)**: Deletes VNet, subnets, and Network Security Group.
5. **Resource Group (`PhaseDownResourceGroup`)**: Preserves the Resource Group by default to protect shared RBAC and policy scopes. Deleted only if `--delete-rg` is explicitly specified.

