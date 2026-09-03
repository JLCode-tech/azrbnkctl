# azrbnkctl Architecture Specification

## 1. System Overview

`azrbnkctl` is a pure Go CLI binary and deployment engine tailored for running **F5 BIG-IP Next for Kubernetes (BNK)** on **Microsoft Azure Kubernetes Service (AKS)**.

It provides deterministic multi-subnet Azure VNet topology orchestration, AKS cluster lifecycle management, DPDK Accelerated Networking node pool configuration, and Kubernetes Dynamic Server-Side Apply (SSA) installation of F5 BNK 2.3.2+ components.

---

## 2. Network Topology & Azure Dataplane

### VNet and Subnet Layout
A dedicated Virtual Network is partitioned into four distinct subnets:
1. **System Subnet (`snet-system`, `10.0.0.0/20`)**:
   - Hosts the AKS default system node pool (`Standard_D4s_v5`).
   - Runs control plane components, FLO operator, cert-manager, CoreDNS, and client workloads.
2. **Public Subnet (`snet-public`, `10.0.16.0/28`)**:
   - Hosts public NAT Gateway and optional Ingress endpoints.
3. **External Subnet (`snet-external`, `10.0.17.0/24`)**:
   - Attached to TMM virtual function (VF 1) as the north-south dataplane network.
   - Provides egress routing and external gateway connectivity (`10.0.17.1`).
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
- Enforces Kubernetes version floor >= `1.32.0`.
- Validates CIDR containment, VM size formatting, and node pool specifications.
- Computes derived resource names (`ResourceGroupName`, `AKSClusterName`, `VNetName`, `NSGName`, `NatGatewayName`).

### 2. Pure Azure ARM SDK v6 Layer (`internal/azure`)
- Pure Go implementation using official `github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/.../v6` packages.
- Zero shell invocations of `az` CLI for cluster operations.
- Interfaced adapters enabling mockability and simulation testing.
- `DefaultAzureCredential` integration with `AZRBNKCTL_SKIP_AUTH=1` bypass.

### 3. Dynamic Server-Side Apply Engine (`internal/k8s`)
- Pure Go implementation using `k8s.io/client-go/dynamic`.
- Server-Side Apply (SSA) with `fieldManager="azrbnkctl"` and `force=true`.
- Zero shell invocations of `kubectl apply`.
- Embedded manifests for `cert-manager v1.16.1`, `01-namespaces.yaml`, `04-issuers.yaml`, `flo-manifest.yaml`.
- Go template renderer for `cneinstance.yaml.tmpl`, `license-cr.yaml.tmpl`, `vlans-routes.yaml.tmpl`, `egress.yaml.tmpl`.

### 4. Phased Lifecycle Engine (`internal/azure/phases`)
- 21 discrete topological phases executed sequentially.
- State persisted to `.azrbnkctl/<cluster-name>/state.env`.
- Safe atomic rollback and reverse-order teardown (`down.go`).
