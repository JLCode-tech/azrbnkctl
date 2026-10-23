# Azure AKS Examples & Reference Architectures

Each directory holds a `cluster.yaml` that `azrbnkctl up` can provision, accompanied by a README that describes its architecture, cost footprint, and verification steps. Copy an example, update the subscription ID and credential paths, and deploy.

`local-zone` is the exception: it contains telco and edge reference manifests to apply to an existing cluster.

| Example | Pattern | ~US$/hr | What it shows |
|---|---|:---:|---|
| [`full-cluster`](full-cluster/) | `dual-interface` | ~$3.50 | **Start here.** Production-grade reference topology: multi-subnet VNet, Accelerated Networking TMM pool, BNK 2.4, NAT gateway, and automated diagnostics. |
| [`dual-interface`](dual-interface/) | `dual-interface` | ~$3.00 | Minimal dual-interface topology separating external ingress and internal east-west data paths. |
| [`external-only`](external-only/) | `external-only` | ~$2.50 | Single external interface deployment where TMM reaches Kubernetes workloads directly across the Azure CNI overlay. |
| [`egress`](egress/) | `external-only` | ~$2.50 | Outbound traffic steering and egress firewalling: pod traffic flips to a BNK-controlled path with AUTOMAP SNAT by applying one `EgressGateway`. |
| [`agentfoundry-demo`](agentfoundry-demo/) | `dual-interface` | ~$4.00 | **AI Agent Tool Governance**: An Azure AI Foundry / Azure OpenAI agent executes FastMCP tool calls governed by the **F5 BNK AI Service Plane**, L4 firewall, Bearer authentication, and rate limiting. |
| [`demo-ai`](demo-ai/) | `dual-interface` | ~$6.00 | **F5 BNK AI Service Plane**: prompt token counting, semantic vector caching, and load-balanced LLM streaming (vLLM / simulator) on AKS. |
| [`local-zone`](local-zone/) | none | $0 | Telco/edge reference manifests (HTTP/2, Diameter TCP 3868, SCTP 9000, and Egress) for Azure Local and Edge Zone deployments. |
| [`local-zone-eastus2`](local-zone-eastus2/) | `dual-interface` | ~$3.00 | Validated edge cluster specification for `eastus2` Azure Local Zone deployment. |
| [`local-zone-westus3`](local-zone-westus3/) | `dual-interface` | ~$3.00 | Edge cluster specification for `westus3` (Phoenix) Azure Edge Zone deployment. |
| [`local-zone-centralus`](local-zone-centralus/) | `dual-interface` | ~$3.00 | Edge cluster specification for `centralus` Azure Edge Zone deployment. |

> [!NOTE]
> Costs are rough Azure East US on-demand compute rates for the VM instances and NAT Gateway, excluding egress bandwidth. Clusters do not scale to zero; run `azrbnkctl down -f <cluster.yaml>` when finished.

---

## Azure VNet & Data Path Layout

Every example builds upon a segregated, deterministic VNet architecture:

```
  VNet: 10.0.0.0/16
  │
  ├── snet-system (10.0.0.0/20) ────────► AKS System Nodes (eth0)
  │                                       ├─ API Server, CoreDNS, FLO Controller
  │                                       └─ Azure CNI Overlay: 10.244.0.0/16
  │
  ├── snet-public (10.0.16.0/28) ───────► Azure Public NAT Gateway
  │                                       └─ 0.0.0.0/0 deterministic public egress
  │
  ├── snet-external (10.0.17.0/24) ─────► BNK External Data Plane (net1 / 1.1)
  │                                       ├─ TMM Self-IP (10.0.17.224 - .254)
  │                                       ├─ Gateway Listener VIPs (10.0.17.100 - .150)
  │                                       └─ Ingress Jumphost / Test Client (.200)
  │
  └── snet-internal (10.0.18.0/24) ─────► BNK Internal Data Plane (net2 / 1.2)
                                          ├─ TMM Internal Self-IP
                                          └─ East-West / Internal Workload Backends
```

The Gateway VIP is an address in the external subnet assigned by the BNK controller. VIPs come from the listener pool (`.100`–`.199`), detailed in [`docs/SCENARIOS.md`](../docs/SCENARIOS.md#8-vip-address-allocation-plan).

---

## Rules Every `cluster.yaml` Follows

1. **Supply-chain credentials are files you supply**:
   - Follow the [F5 BIG-IP Next for Kubernetes CloudDocs](https://clouddocs.f5.com/bigip-next-for-kubernetes/latest/overview.html) to obtain your F5 BNK subscription JWT license (`license.jwt`) and F5 Artifact Registry (`repo.f5.com`) credentials (`bnk.farArchive` and `bnk.jwt`).
   - Both paths are relative to the directory containing your `cluster.yaml` and are gitignored so credentials are never committed.
2. **`metadata.name` uniquely tags resources**:
   - Tags every Azure resource (`azrbnkctl:<name>`) and names the state directory `.azrbnkctl/<name>/`.
3. **Secrets never reside in YAML**:
   - Forge passwords and sensitive tokens are supplied via environment variables (`AZRBNKCTL_FORGE_PASSWORD`, `AZRBNKCTL_FORGE_AGENT_TOKEN`).
4. **Data-plane interfaces avoid name collisions**:
   - Never declare a data plane network named `eth0`. Name secondary networks `external` (`1.1`) and `internal` (`1.2`) to prevent socket collision with TMM's internal gRPC interface.
5. **Gateway API 2.4 compliant**:
   - Every `Gateway` references a corresponding `GatewaySettings` via `infrastructure.parametersRef` and uses `sourceNATConfig.type: Automap` to ensure symmetric return routing.

---

## Pre-flight & Deployment Workflow

Validate configuration intent without making any Azure API calls:
```bash
azrbnkctl validate examples/full-cluster/cluster.yaml
```

Preview resource creation with a non-mutating dry run:
```bash
azrbnkctl up -f examples/full-cluster/cluster.yaml --dry-run
```

Provision the infrastructure and deploy BNK:
```bash
azrbnkctl up -f examples/full-cluster/cluster.yaml
```

Run end-to-end automated validation:
```bash
azrbnkctl scenarios run --all -f examples/full-cluster/cluster.yaml
```

Teardown resources when finished:
```bash
azrbnkctl down -f examples/full-cluster/cluster.yaml
```
