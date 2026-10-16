# azrbnkctl

![BNK](https://img.shields.io/badge/BNK-2.4.0-0a3a5c)
![Kubernetes](https://img.shields.io/badge/Kubernetes-1.32--1.35-326ce5?logo=kubernetes&logoColor=white)
![Azure AKS](https://img.shields.io/badge/Azure-AKS-0078d4?logo=microsoftazure&logoColor=white)
[![CI](https://github.com/JLCode-tech/azrbnkctl/actions/workflows/ci.yml/badge.svg)](https://github.com/JLCode-tech/azrbnkctl/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/JLCode-tech/azrbnkctl?label=download)](https://github.com/JLCode-tech/azrbnkctl/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**Deploy, operate, and benchmark F5 BIG-IP Next for Kubernetes (BNK) on Azure AKS from a single binary.**  
Zero Terraform. Zero host `kubectl`. Zero host `helm`.

---

`azrbnkctl` automates the entire lifecycle of enterprise F5 BNK deployments on Azure Kubernetes Service (AKS). From a single declarative `cluster.yaml`, it provisions the Azure VNet, NAT Gateway, AKS cluster, Accelerated Networking SR-IOV / secondary data-plane NICs, Multus CNI attachments, hugepages, and the full F5 BNK 2.4 software stack. It includes **15 built-in automated traffic validation scenarios**, an enterprise **AI inference benchmarking suite** (`aiperf`), and **AI agent governance walkthroughs** (`examples/agentfoundry-demo/`).

---

## Table of Contents

- [Why azrbnkctl?](#why-azrbnkctl)
- [System Architecture & Network Topology](#system-architecture--network-topology)
- [AI Inference Benchmarking & Comparative Shootouts](#ai-inference-benchmarking--comparative-shootouts)
- [15 Built-in Validation Scenarios](#15-built-in-validation-scenarios)
- [Architecture & Protocol Demos](#architecture--protocol-demos)
- [Quick Start](#quick-start)
- [Interface Patterns](#interface-patterns)
- [Pinned Ecosystem Versions](#pinned-ecosystem-versions)
- [CLI Command Taxonomy](#cli-command-taxonomy)
- [Documentation & The `*bnkctl` Family](#documentation--the-bnkctl-family)

---

## Why azrbnkctl?

Deploying F5 BIG-IP Next for Kubernetes (BNK) on Azure typically requires coordinating multiple infrastructure layers:
- Azure VNet networking with dedicated system, public, external (`snet-external`), and internal (`snet-internal`) subnets, NAT Gateway, and secondary VMSS NICs.
- AKS cluster lifecycle, OIDC identity issuer, and least-privilege Azure Workload Identity / Managed Identities.
- Host-level Linux kernel tuning: 2MB hugepages reservation, Accelerated Networking SR-IOV VF / DPDK attachment, and Multus CNI daemonsets.
- Cloud-native F5 control-plane lifecycle (FLO, CNE, IPAM controllers) and Kubernetes Gateway API resources (`gateway.k8s.f5.com`).
- Live data-plane traffic validation inside private VNets via an in-VNet Azure jumphost VM without exposing workloads to the public internet.

`azrbnkctl` consolidates this entire workflow into a **single, dependency-free binary**:

- **Zero Prerequisites**: No Terraform, Helm, or external `kubectl` required on the host—everything is driven natively via Azure SDK for Go (`armcontainerservice`, `armnetwork`, `armresources`) and `client-go`.
- **Deterministic State Machine**: 27 sequential phases (`00`–`60`) execute with full idempotency, resume safety, and explicit state verification.
- **Built-in Diagnostics & Healing**: The `azrbnkctl bnk heal` command detects and automatically remediates 11 common Kubernetes plumbing issues (Multus token expiration, metrics-server bindings, TMM log streams, EndpointSlice RBAC, and Azure Workload Identity `controller-identity`).
- **In-VNet Validation**: 15 built-in automated test scenarios validate L4/L7 routing, canary splits, gRPC streams, and AI inference directly inside the Azure VNet.
- **Clean Teardown**: Tag-driven resource discovery ensures `azrbnkctl down -f cluster.yaml` destroys provisioned AKS and network resources while preserving the Azure Resource Group by default (unless `--delete-rg` is specified).

---

## System Architecture & Network Topology

`azrbnkctl` provisions an Azure VNet with dedicated network segmentation for management and high-performance TMM traffic:

```mermaid
flowchart LR
    subgraph Mgmt["Management & Access"]
        direction TB
        CLI([Operator CLI]) -->|Azure Jumphost SSH| Jumphost["Azure Jumphost VM<br/>(10.0.17.200)"]
        K8sAPI["AKS Control Plane<br/>(API Server)"]
    end

    subgraph BNK["F5 BNK Data Plane (snet-external: 10.0.17.0/24)"]
        direction TB
        VIP["Gateway VIP Pool<br/>(10.0.17.100 - .117)"]
        TMM["TMM Microkernel<br/>(Accelerated Networking SR-IOV / DPDK)"]
        VIP --> TMM
    end

    subgraph Workloads["AKS Worker Nodes"]
        direction TB
        AppPods["Application Pods<br/>(HTTP / gRPC / L4)"]
        ModelPods["Model Servers<br/>(vLLM / GPU Workers)"]
    end

    subgraph Egress["Outbound Egress"]
        direction TB
        NAT["Azure NAT Gateway"]
        Internet([External APIs / Registries])
        NAT --> Internet
    end

    Jumphost -->|In-VNet Test Traffic| VIP
    TMM -->|Line-Rate L4/L7 Routing| AppPods
    TMM -->|Accelerated Inference Stream| ModelPods
    AppPods -.->|Outbound Egress via VXLAN| TMM
    TMM -.->|SNAT Automap| NAT
```

### Key Architectural Tenets
1. **Isolated Data Path**: TMM attaches to dedicated Azure Accelerated Networking virtual functions (`4567:00:02.0`) and secondary interfaces on `snet-external` (`10.0.17.0/24`) and `snet-internal` (`10.0.18.0/24`) using Multus and DPDK. It bypasses the standard Linux kernel network stack for microsecond packet processing.
2. **Deterministic IP Allocation**: VIPs live in a fixed pool (`.100` through `.117`), the Azure jumphost test client owns `.200`, and TMM self-IPs are dynamically discovered and reconciled across the data-plane subnets.
3. **Transparent Outbound Egress**: Outbound pod traffic to external APIs or registries routes over a VXLAN tunnel (`vlan-int`) directly into TMM, where it is SNAT-translated via Automap to TMM's external self-IP and forwarded to the Azure NAT Gateway. Pod-to-VNet traffic remains local on the node.
4. **Gateway API Native**: BNK 2.4 uses the modern `gateway.k8s.f5.com` API group (`Gateway`, `HTTPRoute`, `L4Route`, `SecPolicy`, `NetPolicy`, `EgressGateway`, and `Infra`), completely replacing legacy 2.3 CRDs.

---

## AI Inference Benchmarking & Comparative Shootouts

`azrbnkctl` embeds an AI performance benchmarking engine powered by NVIDIA's `aiperf`. It measures real-world GenAI latency and throughput from within the private Azure VNet data plane and synchronizes with **BNK Forge** for real-time visualization and comparative analysis.

### Benchmarking Execution Flow

```mermaid
flowchart LR
    subgraph Orchestration["Benchmark Orchestration"]
        direction TB
        UI["Forge Web UI (:3000)<br/>Dashboard & Sweeps"]
        Server["Forge Server (:8000)<br/>Dispatcher & DB"]
        Daemon["azrbnkctl daemon<br/>Agent Listener"]
        UI --> Server
        Server <--> Daemon
    end

    subgraph VNet["Private VNet Data Plane"]
        direction TB
        SSH["Direct SSH / NSG<br/>(Jumphost Tunnel)"]
        JH["In-VNet Jumphost VM<br/>(aiperf Engine)"]
        SSH --> JH
    end

    subgraph Targets["Inference Gateway Targets"]
        direction TB
        BNK["F5 BNK (TMM)"]
        Envoy["Envoy Gateway"]
        HAProxy["HAProxy Ingress"]
        NodePort["Direct Pod IP"]
    end

    subgraph Backend["Inference Engine"]
        LLM[("vLLM Model Server<br/>(GPU Worker)")]
    end

    Daemon -->|Secure Tunnel| SSH
    JH -->|Generated Prompts| Targets
    BNK -->|Inference Stream| LLM
    Envoy -.->|Comparative Run| LLM
    HAProxy -.->|Comparative Run| LLM
    NodePort -.->|Baseline Run| LLM
```

### Built-in Smoke Presets (`--scenarios`)

| Preset | Concurrency | Requests | ISL (Input) | OSL (Output) | Mode | Target Workload |
|---|:---:|:---:|:---:|:---:|:---:|---|
| `latency` | 1 | 50 | 512 | 128 | Stream | Single-user baseline TTFT without queue delay |
| `throughput` | 32 | 500 | 512 | 128 | Batch | Maximum sustained token throughput under load |
| `long-context` | 4 | 50 | 4096 | 512 | Stream | Memory bandwidth & KV cache expansion test |
| `streaming` | 8 | 200 | 512 | 256 | Stream | Conversational multi-user SSE stream delivery |

### Native Forge Scenarios (`--scenario`)

For exhaustive evaluations and performance characterization, `azrbnkctl` supports 8 native synthetic Forge engines plus production trace replay:

- **`baseline`**: Concurrency sweep across 50, 100, 150, 200 concurrent streams.
- **`high-concurrency`**: Heavy prompt pairs up to 300 concurrency with 10k token prompts.
- **`mixed-workload`**: Three-phase adaptive sweep (Warmup $\to$ Short ISL $\to$ Long ISL with prefix sharing).
- **`multi-turn`**: Four-turn conversation simulation with progressively expanding shared prefix (500, 1000, 1500 tokens).
- **`prefix-cache`**: Evaluates KV-cache hit rate and TTFT latency drop across 20 shared prompt pools (80% prefix overlap).
- **`bimodal`**: Two-mode distribution modeling real traffic (70% short 300-token queries, 30% long 4000-token queries).
- **`sustained-load`**: Endurance test running up to 2,500 requests per concurrency step.
- **`burst-recovery`**: 5 rounds alternating between high-load bursts ($c=200$) and low-load probes ($c=25$) to measure queue recovery.
- **`mooncake`**: Open-loop replay of production tool-agent traces with 0.80x time-dilation.

### Multi-Proxy Shootouts (`--proxies`)

Run head-to-head comparisons against Envoy, HAProxy, NGINX, and direct NodePort:

```bash
azrbnkctl benchmark run -f cluster.yaml \
  --proxies f5-bnk,envoy,haproxy,nodeport \
  --scenario baseline,prefix-cache \
  --run-label proxy-shootout
```

### GenAI Metrics Captured
- **TTFT (Time to First Token)**: p50, p90, p95, p99 percentiles.
- **ITL (Inter-Token Latency)**: p50, p95, p99 percentiles for streaming chunks.
- **Token Throughput**: Input, output, and aggregate tokens per second.
- **Prefix Cache Hit Rate**: Real-time GPU KV cache hit ratio delta scraped from vLLM/EPP Prometheus metrics.

*(Detailed documentation, Prometheus scrape configuration, and multi-proxy shootout workflows can be found in [`docs/BENCHMARKS.md`](docs/BENCHMARKS.md).)*

---

## 15 Built-in Validation Scenarios

`azrbnkctl` includes 15 automated validation scenarios covering L4-L7 protocols, hybrid routing, multi-tenancy, AI policies, and outbound egress:

```mermaid
flowchart LR
    JH([Jumphost Test Runner])

    subgraph L7["L7 Ingress"]
        HTTP["http-routing-e2e (.100)"]
        Split["http-traffic-split (.101)"]
        GRPC["grpc-loadbalance (.108)"]
    end

    subgraph L4["L4 Transport"]
        TCP["tcp-l4-loadbalance (.106)"]
        UDP["udp-l4-loadbalance (.107)"]
        PP["proxy-protocol-l4 (.103)"]
    end

    subgraph Hybrid["Hybrid & Multi-Tenancy"]
        Ext["external-resource-pool (.102)"]
        CWC["cluster-wide-watch (.105)"]
        MultiVIP["multi-vip (.115-.117)"]
    end

    subgraph AI["AI Gateway"]
        Tokens["ai-token-counting (.104)"]
        Cache["ai-semantic-cache (.109)"]
        Infer["ai-inference-e2e (.112)"]
    end

    subgraph Diagnostics["Egress & Diagnostics"]
        SNAT["egress-snat (VXLAN)"]
        Core["core-file-collection"]
    end

    JH -->|HTTP / gRPC| L7
    JH -->|TCP / UDP| L4
    JH -->|Hybrid Routing| Hybrid
    JH -->|Inference Streams| AI
    JH -->|Diagnostics| Diagnostics
```

| Category | Scenario | VIP | Verification Method | Status |
|---|---|:---:|---|:---:|
| **L7 Ingress** | `http-routing-e2e` | `.100` | Gateway API HTTPRoute routing to `http-echo` | Green |
| | `http-traffic-split` | `.101` | Weighted canary traffic distribution (80/20 split) | Green |
| | `grpc-loadbalance` | `.108` | gRPC stream load balancing against `kong/grpcbin` | Amber |
| **L4 Transport** | `tcp-l4-loadbalance` | `.106` | Raw TCP proxying via L4Route to nginx marker pods | Green |
| | `udp-l4-loadbalance` | `.107` | Stateless UDP datagram distribution to echo pods | Amber |
| | `proxy-protocol-l4` | `.103` | PROXY protocol v1 header injection via NetPolicy/iRule | Green |
| **Hybrid & Tenancy** | `external-resource-pool` | `.102` | Routing traffic to targets outside AKS (VMs / databases) | Green |
| | `cluster-wide-watch` | `.105` | Dynamic tenant namespace routing via single CNE controller | Green |
| | `cwc-admin-access` | – | Client cert + Bearer token auth on CWC admin endpoints | Green |
| | `multi-vip` | `.115`–`.117` | Multi-VIP isolation and concurrent traffic on single NIC | Green |
| **AI Gateway** | `ai-token-counting` | `.104` | Token quota metering and HTTP 503 overload enforcement | Amber |
| | `ai-semantic-cache` | `.109` | Semantic similarity prompt cache hit verification | Amber |
| | `ai-inference-e2e` | `.112` | End-to-end vLLM Llama-3-8B GPU inference (`--synthetic` for CPU) | Green |
| **Security & Egress** | `egress-snat` | – | Transparent pod egress over VXLAN $\to$ TMM AUTOMAP SNAT | Green |
| | `core-file-collection` | – | CoreMond daemon and host crash directory reconciliation | Green |

*(Complete scenario details, traffic sequence diagrams, and VIP maps: [`docs/SCENARIOS.md`](docs/SCENARIOS.md).)*

---

## Architecture & Protocol Demos

Run automated scenario suites and interactive architecture demonstrations with live verifications:

```bash
# List all registered scenarios
azrbnkctl scenarios list

# Run a live scenario or demo
azrbnkctl scenarios run <scenario-name> -f cluster.yaml
```

### Featured: Azure AI Foundry Agent Tool Governance Demo (`examples/agentfoundry-demo/`)

Demonstrates securing and governing Generative AI agent tool execution: an **Azure AI Foundry** agent invoking Model Context Protocol (MCP) tools through F5 BNK Gateway policies (L4 firewall rules, bearer token validation, tool-level access control, rate limiting, and MCP session persistence).

```mermaid
flowchart LR
    subgraph Callers["AI Agents & Callers"]
        Foundry([Azure AI Foundry Agent<br/>Private VNet Agent])
        ExtCaller([External Caller<br/>Jumphost / Script])
    end

    subgraph Gateway["F5 BNK Security & Governance Gateway (VIP: 10.0.17.100)"]
        direction TB
        FW["1. L4 Firewall Policy<br/>Drop out-of-VNet traffic"]
        Auth["2. Bearer Authentication<br/>Require authorized token"]
        ToolGov["3. Tool Access Rule<br/>Block privileged tools for external"]
        RateLimit["4. Rate Limiting iRule<br/>Enforce request burst limits"]
        SessionPin["5. Session Persistence<br/>Pin MCP session to pod"]

        FW --> Auth --> ToolGov --> RateLimit --> SessionPin
    end

    subgraph ToolBackend["Kubernetes Tool Cluster"]
        MCPPod[("MCP Server Pod<br/>forecast, get_account_balance")]
    end

    subgraph Observability["Telemetry & Audit Stream"]
        Loki["Loki Log Collector"]
        Forge["BNK Forge Dashboard"]
        Loki --> Forge
    end

    Foundry -->|forecast MSFT| FW
    ExtCaller -->|forecast NVDA| FW
    SessionPin -->|Authorized Tool Execution| MCPPod
    Gateway -.->|Stream Audit Events| Loki
```

### Reference Architecture & Protocol Topologies

- **`examples/agentfoundry-demo/`** (VIP `.100`): Full Azure AI Foundry + FastMCP tool governance pipeline with L4 firewall (`F5BigFwPolicy`), L7 rate limiting (`F5BigCneIrule`), MCP session persistence (`F5BigPersistenceProfile`), and Loki/Forge telemetry.
- **`examples/demo-ai/`** (VIP `.112`): Multi-proxy AI inference shootout comparing F5 BNK against Envoy, HAProxy, and direct NodePort with vLLM prefix-cache benchmarking.
- **`examples/egress/`**: Interactive outbound VXLAN egress and firewall policy toggle walkthrough (`EgressGateway` + `SecPolicy`).
- **`examples/local-zone/`**: Telecom and low-latency edge protocol manifests covering Diameter (RFC 6733 over SCTP/TCP) and multiplexed HTTP/2 (`h2c`).

*(All 15 scenarios and protocol architectures include detailed diagrams in [`docs/SCENARIOS.md`](docs/SCENARIOS.md).)*

---

## Quick Start

### 1. Installation

Download the pre-compiled static binary for your architecture from [GitHub Releases](https://github.com/JLCode-tech/azrbnkctl/releases):

```bash
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
curl -fsSL "https://github.com/JLCode-tech/azrbnkctl/releases/latest/download/azrbnkctl_${OS}_${ARCH}.tar.gz" | tar -xz
./azrbnkctl install
azrbnkctl version
```

*(You can upgrade anytime with `azrbnkctl self update`, or build locally via `make build` with Go 1.26+).*

### 2. Configure Your Cluster

Copy the reference example:

```bash
cp -r examples/dual-interface my-cluster && cd my-cluster
```

Edit `cluster.yaml` to set your Azure Subscription ID, location, cluster name, and paths to your F5 FAR container archive and JWT licence token:

```yaml
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: bnk-prod-aks
  subscriptionId: 00000000-0000-0000-0000-000000000000
  location: eastus2
  resourceGroup: azrbnkctl-bnk-prod-aks-rg
pattern: dual-interface
bnk:
  manifestVersion: "2.4.0"
  farArchive: "./keys/f5-far-auth-key.tgz"
  jwt: "./keys/license.jwt"
```

### 3. Validate & Dry-Run

```bash
# Validate schema and intent locally (zero Azure API calls)
azrbnkctl validate cluster.yaml

# Perform a local dry-run plan (skipping Azure credentials check)
AZRBNKCTL_SKIP_AUTH=1 azrbnkctl up -f cluster.yaml --dry-run
```

### 4. Deploy, Test & Destroy

```bash
# 1. Provision VNet, NAT Gateway, AKS cluster, TMM node pool, and BNK (~20 minutes)
azrbnkctl up -f cluster.yaml

# 2. Check cluster and BNK health
azrbnkctl status -f cluster.yaml
azrbnkctl doctor --backend k8s

# 3. Run validation scenarios
azrbnkctl scenarios run http-routing-e2e -f cluster.yaml

# 4. Clean teardown (preserves Resource Group by default; add --delete-rg to remove RG)
azrbnkctl down -f cluster.yaml --yes
```

---

## Interface Patterns

`azrbnkctl` configures the TMM data-plane network interfaces according to your architecture:

| Pattern | TMM Subnets Provisioned | Recommended Use Case |
|---|---|---|
| **`dual-interface`** *(Default)* | External (`snet-external` `10.0.17.0/24`) + Internal (`snet-internal` `10.0.18.0/24`) | Reference topology. Routes traffic to both K8s pod backends and off-cluster enterprise networks with Accelerated Networking SR-IOV. |
| **`external-only`** | External (`snet-external` `10.0.17.0/24`) only | Single-arm ingress and transparent pod egress deployments. |

---

## Pinned Ecosystem Versions

All ecosystem components are pinned to validated, enterprise-tested versions:

| Component | Pinned Version | Config Key / Location | Notes |
|---|:---:|---|---|
| **F5 BNK Release** | `2.4.0` | `bnk.manifestVersion` | Default (`2.4.0-3.2882.0`). Supports 2.3.x overrides. |
| **Kubernetes (AKS)** | `1.34.10` | `cluster.kubernetesVersion` | Validated across `1.32`–`1.35` (preflight rejects < 1.32). |
| **cert-manager** | `v1.16.1` | Embedded upstream YAML | Applied via `client-go` Server-Side Apply without Helm. |
| **FLO Chart** | `v2.30.0-0.5.2` | `manifest.KnownReleases` | Paired per release in embedded BNK BOM. |
| **Go Runtime** | `1.26` | `go.mod` | Compiled with Azure SDK for Go (`armcontainerservice/v6`, `armnetwork/v6`). |

---

## CLI Command Taxonomy

`azrbnkctl` provides 24 top-level commands organized into operational domains:

- **Lifecycle**: `init`, `validate`, `up`, `down`, `status`, `doctor`, `topology`, `kubeconfig`, `version`.
- **Validation Tests**: `test` (`connectivity`, `dns`, `egress`, `traffic`).
- **Data Plane Scenarios**: `scenarios` (`list`, `run`, `clean`) — 15 automated validation scenarios.
- **Kubernetes Passthrough**: `k` (`apply`, `delete`, `describe`, `exec`, `get`, `logs`, `port-forward`), aliases `get` and `logs`.
- **BNK Runtime & Self-Healing**: `bnk` (`heal`, `resync`, `upgrade`, `migrate-2.4`, `mcp-session`), `manifest probe`.
  - `bnk heal`: 11-point automated repair suite resolving `multus-token-watch`, `metrics-server`, `tmm-log-stream`, `pod-manager`, `cwc`, `dssm-probe`, `controller-endpointslices`, `controller-identity`, `tmm-k8s-routes`, `epp-namespaces`, and `test-namespace`.
- **AI & Benchmarking**: `benchmark` (`setup`, `run`, `list`, `status`, `daemon`).
- **Fleet & Forge**: `forge` (`register`, `status`, `unregister`, `cleanup`, `template`, `scan`, `telemetry`, `benchmark`), alias `bnkforge`.
- **Agentic Workflow**: `agent` (`gemini`, `claude`, `opencode`, `cursor`, `all`), `journal` (`add`, `list`).
- **Installation & Maintenance**: `install`, `self` (`check`, `update`), `completion`, `help`.

*(Complete command and flag reference: [`docs/COMMANDS.md`](docs/COMMANDS.md)).*

---

## Documentation & The `*bnkctl` Family

### Deep-Dive Guides
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — Phased state machine (27 phases), CNEInstance / Infra / GatewaySettings models, and Azure network design.
- [`docs/BENCHMARKS.md`](docs/BENCHMARKS.md) — Comprehensive AI inference benchmarking guide, presets, native Forge scenarios, and proxy shootout recipes.
- [`docs/SCENARIOS.md`](docs/SCENARIOS.md) — In-depth breakdown of all 15 scenarios, assertions, and IP plans.
- [`docs/COMMANDS.md`](docs/COMMANDS.md) — Full CLI flags and environment variables reference.
- [`docs/FORGE_INTEGRATION.md`](docs/FORGE_INTEGRATION.md) — Peer-read integration with BNK Forge GUI and telemetry schemas.
- [`docs/UPGRADE-2.4.md`](docs/UPGRADE-2.4.md) — Step-by-step in-place upgrade from BNK 2.3 to 2.4.
- [`docs/ALIGNMENT.md`](docs/ALIGNMENT.md) — Cross-cloud architectural alignment across the `*bnkctl` family.
- [`docs/PHASES.md`](docs/PHASES.md) — Detailed reference of all 27 provisioning and teardown phases.
- [`examples/`](examples/) — Ready-to-deploy reference manifests (`dual-interface`, `external-only`, `full-cluster`, `egress`, `demo-ai`, `agentfoundry-demo`).

### The `*bnkctl` Multi-Cloud Family
`azrbnkctl` is part of a unified family of single-binary CLIs bringing declarative F5 BNK management across clouds and on-premises environments:

| Tool | Cloud / Platform | Data Plane Mechanism | Repository |
|---|---|---|---|
| **`awsbnkctl`** | AWS EKS | Secondary ENIs (host-device / SR-IOV DPDK), Multus | [JLCode-tech/awsbnkctl](https://github.com/JLCode-tech/awsbnkctl) |
| **`gkebnkctl`** | GCP GKE | Secondary VPC interfaces (host-device), Multus | [JLCode-tech/gkebnkctl](https://github.com/JLCode-tech/gkebnkctl) |
| **`azrbnkctl`** | Azure AKS | MANA / Accelerated Networking + Multus + Secondary NICs | [JLCode-tech/azrbnkctl](https://github.com/JLCode-tech/azrbnkctl) |
| **`roksbnkctl`** | IBM Cloud ROKS / OpenShift | IBM Cloud VPC secondary subnets, Calico / OVN | [jgruberf5/roksbnkctl](https://github.com/jgruberf5/roksbnkctl) |
| **`ocibnkctl`** | Local OCI / k3s | Container netns virtio demo mode, Anycast BGP | [mwiget/ocibnkctl](https://github.com/mwiget/ocibnkctl) |
| **`bnkctl`** | Multi-Cloud Unified CLI | Unified F5 BNK lifecycle dispatcher | [JLCode-tech/bnkctl](https://github.com/JLCode-tech/bnkctl) |
| **`bnkctl-index`** | BNK Forge Catalog | Unified Forge runner module index | [mwiget/bnkctl-index](https://github.com/mwiget/bnkctl-index) |

---

## Contributing & License

Contributions are welcome! Please review [`CONTRIBUTING.md`](CONTRIBUTING.md) for local quality gates (`gofmt`, `go vet`, `staticcheck`, `gosec`, `go test -race`).

Licensed under the [MIT License](LICENSE). © 2026 JLCode-tech.
