# azrbnkctl Architecture Specification

## 1. System Overview

`azrbnkctl` is a pure Go CLI binary and deployment engine tailored for running **F5 BIG-IP Next for Kubernetes (BNK 2.4.0, with backward compatibility for 2.3.x)** on **Microsoft Azure Kubernetes Service (AKS)**.

It provides deterministic multi-subnet Azure VNet topology orchestration, AKS cluster lifecycle management, DPDK Accelerated Networking node pool configuration, and Kubernetes Dynamic Server-Side Apply (SSA) installation of F5 BNK 2.4 components (`CNEInstance`, `Infra`, `GatewaySettings`, `EgressGateway`, Gateway API, and GIE CRDs).

---

## 2. Network Topology & Azure Dataplane

### VNet and Subnet Layout
A dedicated Virtual Network is partitioned into four distinct subnets:
1. **System Subnet (`snet-system`, `10.0.0.0/20`)**:
   - Hosts the AKS default system node pool (`Standard_D4s_v5`).
   - Runs control plane components, FLO operator, cert-manager, CoreDNS, and client workloads.
2. **Public Subnet (`snet-public`, `10.0.16.0/24`)**:
   - Hosts public NAT Gateway, optional jumphost VM, and public ingress endpoints.
3. **External Subnet (`snet-external`, `10.0.17.0/24`)**:
   - Attached to TMM virtual function (VF 1) as the north-south dataplane network.
   - Provides egress routing, external gateway connectivity (`10.0.17.1`), and BNK VIP listener pool (`10.0.17.100`–`10.0.17.199`).
4. **Internal Subnet (`snet-internal`, `10.0.18.0/24`)** *(in dual-interface mode)*:
   - Attached to TMM virtual function (VF 2) as the east-west dataplane network.
   - Connected to internal services and backends.

### Accelerated Networking (SR-IOV / DPDK)
- TMM worker node pools utilize Azure Accelerated Networking (`Standard_D8s_v5` or larger).
- Mellanox / Azure SR-IOV virtual functions are passed directly into F5 TMM DPDK driver space (`0001:00:01.0`, `0001:00:02.0`).

---

## 3. Core Subsystems

### 1. Intent Engine (`internal/intent`)
- Decodes `cluster.yaml` (`azrbnkctl/v1`).
- Enforces Kubernetes version floor >= `1.32.0` (default `1.34.10`) and defaults BNK `manifestVersion` to `"2.4.0"` (`deploymentSize: "small"`).
- Validates CIDR containment, VM size formatting, and node pool specifications.
- Computes derived resource names (`ResourceGroupName`, `AKSClusterName`, `VNetName`, `NSGName`, `NatGatewayName`).

### 2. Pure Azure ARM SDK v6 Layer (`internal/azure`)
- Pure Go implementation using official `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/.../v6` packages.
- Zero shell invocations of `az` CLI for cluster operations.
- Interfaced adapters enabling mockability and simulation testing.
- `DefaultAzureCredential` integration with `AZRBNKCTL_SKIP_AUTH=1` bypass.

### 3. Dynamic Server-Side Apply Engine (`internal/k8s`)
- Pure Go implementation using `k8s.io/client-go/dynamic` and embedded Helm v3 SDK (`f5-lifecycle-operator`).
- Server-Side Apply (SSA) with `fieldManager="azrbnkctl"` and `force=true`.
- Zero shell invocations of `kubectl apply`.
- Embedded manifests for `cert-manager v1.16.1`, Gateway API `v1.2.1`, Gateway API Inference Extension (GIE) `v1.2.0`, and BNK 2.4 `gateway.k8s.f5.com/v1alpha1` CRDs (`Infra`, `L2Network`, `L3Network`, `GatewaySettings`, `EgressGateway`, `Backend`).
- Go template renderer for `cneinstance.yaml.tmpl`, `infra.yaml.tmpl`, `egress-gateway.yaml.tmpl`, `license-cr.yaml.tmpl`, and legacy 2.3 templates (`vlans-routes.yaml.tmpl`, `egress.yaml.tmpl`).

### 4. Phased Lifecycle & Self-Heal Engine (`internal/azure/phases`)
- **27 discrete topological `up` phases** (`00` through `50`, including `29` `forge-register`, `31b` `metrics-server`, and `38`/`38b`/`38c`/`38d` post-install heal phases) executed sequentially.
- Shared 10-repair self-heal registry (`internal/azure/phases/heal.go`) invoked by `azrbnkctl up`, `azrbnkctl doctor`, `azrbnkctl bnk heal`, and `azrbnkctl bnk upgrade`.
- State persisted to `.azrbnkctl/<cluster-name>/state.env`.
- Safe atomic rollback and reverse-order teardown (`down.go`).
