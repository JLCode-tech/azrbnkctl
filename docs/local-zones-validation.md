# Azure Local & Edge Zones: Validation & Engineering Notes

This document summarizes engineering validation and operational considerations for deploying F5 BIG-IP Next for Kubernetes (BNK) 2.4 on **Azure Kubernetes Service (AKS)** across **Azure Local (formerly Azure Stack HCI / Azure Arc)**, **Azure Edge Zones**, and regional edge topologies.

The validated reference architecture is demonstrated in [`examples/local-zone/`](../examples/local-zone/), [`examples/local-zone-eastus2/`](../examples/local-zone-eastus2/), and detailed in [`docs/AZURE_AKS_BNK24_DEPLOYMENT_REPORT.md`](AZURE_AKS_BNK24_DEPLOYMENT_REPORT.md).

---

## Architecture & Subsystem Matrix

| Area | Challenge / Finding | Architectural Solution in `azrbnkctl` |
|---|---|---|
| **Data Plane Driver** | Azure Hyper-V / NetVSC interfaces do not expose raw PCIe SR-IOV VFs to unprivileged containers | Configured TMM in **Generic Socket Driver** mode (`TMM_GENERIC_SOCKET_DRIVER=true`, raw `AF_PACKET` sockets, `PAL_CPU_SET=0,2`). |
| **Interface Naming & Namespace Clashes** | TMM pod management interface inside container is named `eth0` (`0.1`, `xeth0`) for gRPC on `:8750`. Naming the external data plane network `eth0` triggers fatal collisions in CNE. | Explicitly name secondary data plane networks `external` (`net1` / `1.1`) and `internal` (`net2` / `1.2`) in Multus NADs, `Infra`, and `GatewaySettings` CRDs. |
| **Multus Sandbox Tokens** | On node restarts or CNI re-initializations, Multus can leave orphaned token files causing `Unauthorized` errors. | Deployed Multus CNI daemonset v4.2.4 with `--cleanup-config-on-exit=true`. |
| **Asymmetric Routing Across Hops** | In multi-subnet edge VNets, backend pods reply via the default system subnet gateway instead of TMM if destination IP is external client IP. | Required `sourceNATConfig.type: Automap` on all edge `GatewaySettings` so backend return packets route deterministically to TMM. |
| **VM SKU & Accelerated Networking** | High packet-per-second workloads at the edge require HW-accelerated virtual switches. | Selected `Standard_D8s_v5` / `Standard_D8s_v3` with `acceleratedNetworking: true` enabled on the TMM agent pool. |
| **Outbound Egress & NAT** | Edge nodes require deterministic public egress for image pulling, license validation, and outbound telemetry without per-node public IPs. | Provisioned dedicated `snet-public` (`10.0.16.0/28`) with an Azure NAT Gateway attached. |
| **Gateway API Listener Protocols** | Gateway API v1 core spec standardizes HTTP, HTTPS, TLS, TCP, UDP. Telecom SCTP requires custom CRD handling. | L4 TCP and UDP routes use `L4Route`; SCTP is documented as a reference manifest for telco 5G edge deployments. |

---

## 1. Network Layout for Edge & Local Zones

```
  snet-system (10.0.0.0/20) ───────────► AKS System Node Pool (API server, CoreDNS, FLO)
  snet-public (10.0.16.0/28) ──────────► Azure Public NAT Gateway (0.0.0.0/0 Egress)
  
  snet-external (10.0.17.0/24) ────────► BNK External Data Plane (net1 / 1.1)
  │                                      ├─ TMM Self-IP (10.0.17.224 - .254)
  │                                      ├─ Gateway VIPs (.100 - .150)
  │                                      └─ Edge Jumphost / Ingress Clients
  │
  snet-internal (10.0.18.0/24) ────────► BNK Internal Data Plane (net2 / 1.2)
                                         ├─ TMM Internal Self-IP
                                         └─ Core East-West Workload Backends
```

---

## 2. Interface Mapping Rules

When authoring custom manifests for Azure Local or Edge Zones:

1. **Never use `eth0` for data plane network names**:
   ```yaml
   # INCORRECT (causes internal socket collision):
   networkRefs:
   - name: eth0

   # CORRECT:
   networkRefs:
   - name: ext-vlan-infra # or external
   ```

2. **Always include Automap SNAT on Edge Listeners**:
   ```yaml
   apiVersion: gateway.k8s.f5.com/v1alpha1
   kind: GatewaySettings
   metadata:
     name: edge-settings
     namespace: edge-workloads
   spec:
     ingressConfig:
       defaultListenerNetwork:
         ipamRefs:
         - name: listener-pool
         networkRefs:
         - name: ext-vlan-infra
         sourceNATConfig:
           type: Automap
   ```

3. **Verify Accelerated Networking**:
   Before running high-throughput scenarios, verify that `ethtool -S <iface> | grep vf_rx_packets` on the AKS node increments, confirming Azure Accelerated Networking synthetic-VF offload is active.
