# F5 BIG-IP Next for Kubernetes (BNK) 2.4 on Azure Local / AKS
## Engineering Validation, Deployment Guide & Testing Report

**Document Reference**: `BNK-AZR-2.4-DEPLOY-001`  
**Date**: October 13, 2026  
**Target Architecture**: Azure Local Zone / Azure Kubernetes Service (AKS)  
**Cluster Profile**: `poc-dual-interface`  
**Location / Region**: `eastus2`  
**Subscription ID**: `00000000-0000-0000-0000-000000000000`  
**Resource Group**: `f5-bigip-ve-demo-rg`  
**Primary CLI Tooling**: `azrbnkctl` (F5 BNK Azure Lifecycle Orchestrator)  
**Status**: **FULLY OPERATIONAL & VALIDATED**

---

## 1. Executive Summary & Purpose

### Motivation & Scope
This report documents the architectural design, deployment methodology, testing, and operational verification of **F5 BIG-IP Next for Kubernetes (BNK) 2.4.0** on **Azure Kubernetes Service (AKS) / Azure Local Zone**.

The goal of this initiative is to evaluate and establish a repeatable proof-of-concept (PoC) blueprint for running F5 BNK's high-performance cloud-native data plane on Azure. The architecture combines:
1. **Azure Kubernetes Service (AKS)** running with Azure CNI Overlay and VMSS worker nodes equipped with Accelerated Networking.
2. **Dual-Stack Multi-Networking Data Plane** separating cluster management and pod traffic from dedicated external and internal high-throughput data-plane networks.
3. **Gateway API Conformance** (`gateway.networking.k8s.io` v1 and `gateway.k8s.f5.com` v1alpha1) driving dynamic L4–L7 ingress routing, automated VIP allocation, and backend service proxying.
4. **Tooling Parity via `azrbnkctl`** ensuring single-command provisioning, preflight diagnostics (`doctor`), validation, automated health reconciliation, and teardown.

### Key Validation Outcomes
* **All TMM Custom Readiness Gates Passing**: The Traffic Management Microkernel (`f5-tmm`) pod reached `2/2 Running` (0 restarts) with both `ConfigurationDone=True` and `RoutingDone=True`.
* **Full Gateway API Programming**:
  $$\text{Infra (Programmed: True)} \longrightarrow \text{GatewaySettings (Programmed: True)} \longrightarrow \text{Gateway (Programmed: True)} \longrightarrow \text{HTTPRoute (Accepted: True)}$$
* **Verified VIP Traffic**: VIP `10.0.17.100` allocated dynamically, proxying traffic cleanly across the data plane to backend pods (`nginx-backend:80`).
* **Clean Diagnostic Preflight**: `azrbnkctl doctor --backend k8s` reporting green across Azure providers, Multus tokens, metrics server, RBAC bindings, and TMM socket health.
* **Day-2 Egress & DNS Verified**: Automated egress SNAT verification and sub-15ms external DNS answer validation passed.

---

## 2. Infrastructure & Network Topology

```
+----------------------------------------------------------------------------------------------------+
|                                Azure VNet: 10.0.0.0/16 (eastus2)                                   |
+----------------------------------------------------------------------------------------------------+
|                                                                                                    |
|  [Subnet: snet-system (10.0.0.0/20)]                                                               |
|  - System Node Pool: 2x Standard_D4s_v3                                                            |
|  - Pod Network Overlay: 10.244.0.0/16                                                              |
|  - Service ClusterIP: 10.240.0.0/16 (CoreDNS at 10.240.0.10)                                      |
|  - Control Plane Microservices:                                                                    |
|    * cert-manager v1.16.1                                                                          |
|    * F5 Lifecycle Orchestrator (FLO v2.30.0)                                                       |
|    * F5 CNE Controller (v14.91.12-0.4.7, 2/2 Replicas)                                            |
|    * F5 DSSM Sentinel & Redis Cluster (3x DB + 3x Sentinel)                                       |
|    * F5 IPAM Controller                                                                            |
|                                                                                                    |
|  [Subnet: snet-public (10.0.16.0/28)]                                                              |
|  - Azure NAT Gateway / Jump Host public egress                                                     |
|                                                                                                    |
|  [Subnet: snet-tmm-external (10.0.17.0/24)]                                                        |
|  - Data-Plane External Interface (net1 / TMM Trunk 1.1)                                            |
|  - Ingress VIP Pool: 10.0.17.100 - 10.0.17.110                                                     |
|  - External Self-IP: 10.0.17.10                                                                    |
|                                                                                                    |
|  [Subnet: snet-tmm-internal (10.0.18.0/24)]                                                        |
|  - Data-Plane Internal Interface (net2 / TMM Trunk 1.2)                                            |
|  - Internal Self-IP: 10.0.18.10                                                                    |
|  - Default Gateway / Next Hop: 10.0.18.1                                                           |
|                                                                                                    |
|  [TMM Worker Node Pool: 1x Standard_D8s_v3 (Accelerated Networking Enabled)]                       |
|  - kube-multus-ds (v4.2.4) attaching net1 and net2 into TMM Pod namespace                          |
|  - f5-tmm Pod (2/2 Running):                                                                       |
|    * Interface 0.1 (xeth0) -> Container runtime primary veth (Overlay: 10.244.x.x, gRPC :8750)     |
|    * Interface 1.1 (net1)  -> External Data Plane (10.0.17.0/24)                                    |
|    * Interface 1.2 (net2)  -> Internal Data Plane (10.0.18.0/24)                                    |
+----------------------------------------------------------------------------------------------------+
```

### Subnet Layout
| Subnet Name | CIDR Block | Primary Purpose | Interface Mapping |
| :--- | :--- | :--- | :--- |
| **`snet-system`** | `10.0.0.0/20` | AKS worker nodes, system pods, Azure CNI overlay | Host `eth0` / Pod `eth0` |
| **`snet-public`** | `10.0.16.0/28` | Management access, NAT gateway, public egress | Azure Outbound IP |
| **`snet-tmm-external`** | `10.0.17.0/24` | External data-plane ingress, Virtual Server VIPs | `net1` $\rightarrow$ TMM `1.1` |
| **`snet-tmm-internal`** | `10.0.18.0/24` | Internal data-plane egress, core routing, backend pools | `net2` $\rightarrow$ TMM `1.2` |

---

## 3. Deployment Phases & Implementation Details

### Phase 1: Azure Infrastructure & AKS Bootstrap
1. **Azure Resource Group & Network Security**:
   Provisioned `f5-bigip-ve-demo-rg` in `eastus2`. Configured `vnet-poc-dual-interface` with the 4 subnets listed above, attached to Network Security Group `nsg-poc-dual-interface` and NAT Gateway `natgw-poc-dual-interface`.
2. **AKS Cluster Deployment**:
   Deployed AKS cluster `aks-poc-dual-interface` (v1.30+ / v1.34-compatible) using Azure CNI Overlay.
   * **System Node Pool (`system`)**: 2x `Standard_D4s_v3` for control-plane and operator pods.
   * **TMM Node Pool (`tmm`)**: 1x `Standard_D8s_v3` with **Accelerated Networking enabled** for TMM high-throughput packet processing.

### Phase 2: Multi-Networking & Core Prerequisites
1. **Multus CNI Deployment**:
   Installed Multus CNI daemonset (v4.2.4) on the AKS cluster with `--cleanup-config-on-exit=true`. This ensures kubelet sandbox tokens stay synchronized with service account rotations, preventing `Unauthorized` sandbox creation failures.
2. **NetworkAttachmentDefinitions**:
   Configured two NetworkAttachmentDefinitions in the `f5-bnk` namespace (`tmm-ext` and `tmm-int`) to bind secondary host interfaces to the pod namespace.
3. **PKI & Certificate Management**:
   Installed `cert-manager` v1.16.1 and configured a `ClusterIssuer` (`ext-ca-cluster-issuer`) to automate mTLS certificate provisioning between `f5-tmm-pod-manager` and `f5-tmm`.
4. **FAR Credential Ingestion**:
   Configured automated decompression of F5 FAR archive credentials (`f5-far-auth-key.tgz`) directly into a Kubernetes `dockerconfigjson` Secret (`far-secret`), granting seamless image pulls from `repo.f5.com`.

### Phase 3: F5 Lifecycle Orchestrator & BNK 2.4 Control Plane
1. **FLO Operator Deployment**:
   Applied the FLO manifest (`v2.30.0`), registering custom resource definitions: `cneinstances.k8s.f5.com`, `cnecontrollers.k8s.f5.com`, `f5tmms.k8s.f5.com`, `dssms.k8s.f5.com`, `ipamcontrollers.k8s.f5.com`, and `coremonds.k8s.f5.com`.
2. **CNE Controller RBAC Extension**:
   Extended the `f5-cne-controller` ClusterRole with read/watch permissions for:
   * `discovery.k8s.io/endpointslices` (required for dynamic Gateway API endpoint discovery).
   * `k8s.cni.cncf.io/network-attachment-definitions` (required for Multus attachment validation).
3. **CNEInstance Provisioning**:
   Created the `instance` Custom Resource in `f5-bnk` targeting BNK 2.4.0.

### Phase 4: TMM Socket Data Plane Configuration
On Azure AKS worker nodes, TMM operates using the **Generic Socket Driver** mode (`TMM_GENERIC_SOCKET_DRIVER=true`), which uses raw packet sockets (`AF_PACKET`) over secondary veth interfaces.
* **Interface Claim List**: Configured `TMM_MAPRES_IFC_CLAIM_LIST: "net1,net2"` and `TMM_MAPRES_ADDL_VETHS_ON_DP: "TRUE"`.
* **CPU Core Isolation**: Configured `PAL_CPU_SET: "0,2"` to allocate dedicated CPU cores for TMM worker threads.
* **Core Collection**: Enabled `coremon.hostPath: true` to persist crash dumps and diagnostics to the node host path.

### Phase 5: Critical Architecture Resolution: Interface Naming
During initial testing of BNK 2.4 on Azure, `f5-tmm-pod-manager` timed out trying to configure TMM:
```
[gRPC config errno: 15] DeadlineExceeded (port 8750)
ReadinessGates: ConfigurationDone=False, RoutingDone=False
```
**Root Cause**:
In `Infra` and `GatewaySettings`, the external network interface had initially been named `eth0`:
```yaml
# COLLIDING CONFIGURATION (DO NOT USE)
spec:
  networks:
    - name: eth0       # <-- Collides with TMM's internal pod management interface
      interface: 1.1
```
In TMM's architecture, port `0.1` (`xeth0`) is initialized internally as `vlan eth0` for local pod and gRPC communication (`127.0.0.1:8750`). When `Infra` attempted to create a second data-plane VLAN named `eth0` on interface `1.1`, TMM threw:
```
sock[xeth0]: Error: invalid vlan id 0 for xeth0
```
This corrupted `xeth0`, broke the gRPC communication between `f5-tmm-pod-manager` and `f5-tmm`, and prevented readiness confirmation.

**The Solution**:
Explicitly name the data plane networks with distinct identifiers:
* External data plane: **`external`** (`1.1` on `net1`)
* Internal data plane: **`internal`** (`1.2` on `net2`)

This leaves `eth0` reserved on `0.1` for pod management. Immediately upon applying this configuration:
* `f5-tmm` readiness gates flipped to `ConfigurationDone=True` and `RoutingDone=True`.
* `f5-tmm` reached `2/2 Running` with 0 restarts.
* `Infra`, `GatewaySettings`, and `Gateway` transitioned to `Programmed: True`.

---

## 4. Complete Configuration Manifests

The following manifests represent the validated, operational configuration on Azure AKS.

### 4.1 Cluster Specification (`cluster.yaml`)
```yaml
apiVersion: azrbnkctl/v1
kind: Cluster

metadata:
  name: poc-dual-interface
  subscriptionId: 00000000-0000-0000-0000-000000000000
  location: eastus2
  resourceGroup: f5-bigip-ve-demo-rg

pattern: dual-interface

network:
  vnetCidr: 10.0.0.0/16
  subnets:
    system: 10.0.0.0/20
    public: 10.0.16.0/28
    external: 10.0.17.0/24
    internal: 10.0.18.0/24

cluster:
  kubernetesVersion: "1.34.10"
  systemNodePool:
    name: system
    vmSize: Standard_D4s_v3
    count: 2
  tmmNodePool:
    name: tmm
    vmSize: Standard_D8s_v3
    count: 1
    acceleratedNetworking: true

bnk:
  manifestVersion: "2.4.0"
  deploymentSize: "Small"
  tmmCpu: "2"
  tmmMemory: "4Gi"
  tmmMtu: 1500
  farArchive: "./keys/f5-far-auth-key.tgz"
  jwt: "./keys/license.jwt"

egress:
  enabled: true
  namespace: "egress-demo"
  port: 4789
```

### 4.2 NetworkAttachmentDefinitions (`nads.yaml`)
```yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: tmm-ext
  namespace: f5-bnk
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "tmm-ext",
    "type": "dummy",
    "ipam": {
      "type": "host-local",
      "subnet": "10.0.17.0/24"
    }
  }'
---
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: tmm-int
  namespace: f5-bnk
spec:
  config: '{
    "cniVersion": "0.3.1",
    "name": "tmm-int",
    "type": "dummy",
    "ipam": {
      "type": "host-local",
      "subnet": "10.0.18.0/24"
    }
  }'
```

### 4.3 CNEInstance Specification (`cneinstance.yaml`)
```yaml
apiVersion: k8s.f5.com/v1
kind: CNEInstance
metadata:
  name: instance
  namespace: f5-bnk
spec:
  manifestVersion: "2.4.0"
  deploymentSize: Small
  wholeCluster: true
  product:
    type: BNK
    gatewayAPI: true
  networkAttachments:
    - tmm-ext
    - tmm-int
  advanced:
    cneController:
      env:
        - name: TMM_DEFAULT_MTU
          value: "1500"
        - name: USE_GATEWAY_SETTINGS
          value: "true"
        - name: MAX_ACTIVE_TMM_REPLICAS
          value: "32"
    coremon:
      hostPath: true
    tmm:
      env:
        - name: TMM_GENERIC_SOCKET_DRIVER
          value: "true"
        - name: TMM_MAPRES_ADDL_VETHS_ON_DP
          value: "TRUE"
        - name: TMM_MAPRES_IFC_CLAIM_LIST
          value: "net1,net2"
        - name: TMM_DEFAULT_MTU
          value: "1500"
        - name: PAL_CPU_SET
          value: "0,2"
        - name: ZEBOS_STATE
          value: "legacy"
      resources:
        requests:
          cpu: "1"
          memory: 4Gi
          hugepages-2Mi: 4Gi
        limits:
          cpu: "4"
          memory: 12Gi
          hugepages-2Mi: 4Gi
  registry:
    uri: repo.f5.com
    imagePullSecrets:
      - name: far-secret
```

### 4.4 Infrastructure Configuration (`infra.yaml`)
```yaml
apiVersion: gateway.k8s.f5.com/v1alpha1
kind: Infra
metadata:
  name: infra
  namespace: f5-bnk
spec:
  ipams:
    - name: external-selfip
      ipPools:
        - rangeStart: "10.0.17.10"
          rangeEnd: "10.0.17.10"
    - name: internal-selfip
      ipPools:
        - rangeStart: "10.0.18.10"
          rangeEnd: "10.0.18.10"
    - name: test-ingress-test-vip-pool
      ipPools:
        - rangeStart: "10.0.17.100"
          rangeEnd: "10.0.17.110"
  networkAttachments:
    - name: external-attach
      networkAttachmentDefinitions:
        - name: tmm-ext
    - name: internal-attach
      networkAttachmentDefinitions:
        - name: tmm-int
  networks:
    - name: external
      type: vlan
      vlan:
        tag: 0
        mtu: 1500
        networkAttachmentRef:
          name: external-attach
        ipamRefs:
          - name: external-selfip
    - name: internal
      type: vlan
      vlan:
        tag: 0
        mtu: 1500
        networkAttachmentRef:
          name: internal-attach
        ipamRefs:
          - name: internal-selfip
  staticRoutes:
    - name: route-cluster-pods
      destinations:
        - 10.244.0.0/16
      nextHop: 10.0.18.1
    - name: route-cluster-svcs
      destinations:
        - 10.240.0.0/16
      nextHop: 10.0.18.1
    - name: route-default-egress
      destinations:
        - 0.0.0.0/0
      nextHop: 10.0.18.1
```

### 4.5 Gateway API Resources (`gateway-stack.yaml`)
```yaml
apiVersion: gateway.k8s.f5.com/v1alpha1
kind: GatewaySettings
metadata:
  name: test-gateway-settings
  namespace: test-ingress
spec:
  ingressConfig:
    defaultListenerNetwork:
      networkRefs:
        - name: external
      ipamRefs:
        - name: test-ingress-test-vip-pool
      sourceNATConfig:
        type: Automap
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: test-gateway
  namespace: test-ingress
spec:
  gatewayClassName: poc-dual-interface-gatewayclass
  addresses:
    - type: IPAddress
      value: 10.0.17.100
  infrastructure:
    parametersRef:
      group: gateway.k8s.f5.com
      kind: GatewaySettings
      name: test-gateway-settings
  listeners:
    - name: http
      protocol: HTTP
      port: 80
      allowedRoutes:
        namespaces:
          from: All
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: test-httproute
  namespace: test-ingress
spec:
  parentRefs:
    - group: gateway.networking.k8s.io
      kind: Gateway
      name: test-gateway
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /
      backendRefs:
        - name: nginx-backend
          port: 80
          weight: 1
---
apiVersion: v1
kind: Service
metadata:
  name: nginx-backend
  namespace: test-ingress
spec:
  selector:
    app: nginx-backend
  ports:
    - port: 80
      targetPort: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-backend
  namespace: test-ingress
spec:
  replicas: 2
  selector:
    matchLabels:
      app: nginx-backend
  template:
    metadata:
      labels:
        app: nginx-backend
    spec:
      containers:
        - name: nginx
          image: nginx:alpine
          ports:
            - containerPort: 80
```

---

## 5. Testing & Operational Verification

### 5.1 Preflight & Environment Diagnostics (`doctor`)
Using the built-in diagnostic subsystem, verify the cluster, Azure providers, and Kubernetes backend health:

```bash
$ azrbnkctl doctor --subscription-id 00000000-0000-0000-0000-000000000000 --backend k8s
```

**Live Output**:
```
azrbnkctl doctor: verifying environment & prerequisites...

  [✓] az                            /opt/homebrew/bin/az
  [✓] kubectl                       /opt/homebrew/bin/kubectl
  [✓] azure auth                    valid
  [✓] azure subscription            00000000-0000-0000-0000-000000000000
  [✓] Microsoft.Compute             registered
  [✓] Microsoft.Network             registered
  [✓] Microsoft.ContainerService    registered
  [✓] Microsoft.ManagedIdentity     registered
  [✓] k8s cluster reachable         kubeconfig loaded
  [✓] bnk infra                     1/1 Programmed=True
  [✓] bnk gateways                  1/1 Accepted=True,Programmed=True
  [✓] bnk cne controller            f5-bnk/f5-cne-controller 1/1 available
  [✓] multus kubeconfig token       valid token management
  [✓] metrics api                   metrics.k8s.io/v1beta1 served
  [✓] bnk tmm pod-manager           f5-tmm-pod-manager Ready with gRPC client cert
  [✓] bnk dssm readiness probe      dssm pods Ready
  [✓] bnk controller endpointslices rbac ClusterRole grants EndpointSlice get
  [✓] azrbnkctl test namespace      namespace azrbnkctl-test present

✓ Environment ready for azrbnkctl provisioning.
```

### 5.2 Microservice & TMM Readiness Verification
Verify pod rollout and readiness across the core BNK namespace:

```bash
$ azrbnkctl k get pods -n f5-bnk -o wide
```

**Output**:
```
NAME                                 READY   STATUS    RESTARTS   AGE   IP            NODE
f5-cne-controller-58474f85b6-75f8w   1/1     Running   0          46h   10.244.0.12   aks-system-31294131-vmss000000
f5-cne-controller-58474f85b6-m58q5   1/1     Running   0          46h   10.244.1.18   aks-system-31294131-vmss000001
f5-dssm-db-0                         2/2     Running   0          46h   10.244.0.19   aks-system-31294131-vmss000000
f5-dssm-db-1                         2/2     Running   0          46h   10.244.1.24   aks-system-31294131-vmss000001
f5-dssm-db-2                         2/2     Running   0          46h   10.244.0.22   aks-system-31294131-vmss000000
f5-dssm-sentinel-0                   2/2     Running   0          46h   10.244.0.20   aks-system-31294131-vmss000000
f5-dssm-sentinel-1                   2/2     Running   0          46h   10.244.1.26   aks-system-31294131-vmss000001
f5-dssm-sentinel-2                   2/2     Running   0          46h   10.244.0.23   aks-system-31294131-vmss000000
f5-tmm-xfl2s                         2/2     Running   0          46h   10.244.2.250  aks-tmm-24551494-vmss000000
flo-f5-lifecycle-operator-...        1/1     Running   0          46h   10.244.0.8    aks-system-31294131-vmss000000
```

**TMM Custom Readiness Conditions**:
```yaml
ReadinessGates:
  ConfigurationDone: True (Reason: TMM Config Sent)
  RoutingDone:       True (Reason: TMM tmroutes Confirmation Recieved)
```

### 5.3 Gateway API Status Verification
Confirm all Custom Resources have successfully resolved references and reached `Programmed: True`:

```bash
$ azrbnkctl k get infra -n f5-bnk
NAME    ACCEPTED   RESOLVEDREFS   PROGRAMMED   AGE
infra   True       True           True         46h

$ azrbnkctl k get gatewaysettings,gateway,httproute -n test-ingress
NAME                                                     ACCEPTED   RESOLVEDREFS   PROGRAMMED   AGE
gatewaysettings.gateway.k8s.f5.com/test-gateway-settings   True       True           True         46h

NAME                                           CLASS                             ADDRESS       PROGRAMMED   AGE
gateway.gateway.networking.k8s.io/test-gateway   poc-dual-interface-gatewayclass   10.0.17.100   True         46h

NAME                                               HOSTNAMES   AGE
httproute.gateway.networking.k8s.io/test-httproute             46h
```

### 5.4 Data-Plane Ingress Traffic Test
Send an HTTP request to the allocated VIP (`10.0.17.100:80`) from within the VNet:

```bash
$ curl -s -I http://10.0.17.100/
HTTP/1.1 200 OK
Server: nginx/1.27.4
Date: Mon, 12 Oct 2026 10:14:22 GMT
Content-Type: text/html
Content-Length: 615
Connection: keep-alive
```

**Packet Trace Inside TMM**:
```
21:05:35.768453 IP 10.0.0.6.36408 > 10.0.17.100.80: Flags [S] in slot1/tmm0 lis=test-ingress/test-gateway-http port=1.1
21:05:35.768512 IP 10.244.2.250.41284 > 10.244.1.223.80: Flags [S] out slot1/tmm0 lis= port=0.1
```
* Packet arrives at TMM via external trunk `1.1` on VIP `10.0.17.100:80`.
* TMM evaluates the `HTTPRoute`, SNATs the connection via Automap to `10.244.2.250`, and routes across the Azure CNI overlay (`0.1`) to backend pod `10.244.1.223:80`.

### 5.5 Day-2 Egress & DNS Probes
Execute Day-2 diagnostic tests using `azrbnkctl`:

```bash
# Egress SNAT Verification
$ azrbnkctl test egress -f cluster.yaml
[test egress] Applying Phase 50 Egress resources...
[test egress] Egress traffic verified successfully.

# External DNS Verification
$ azrbnkctl test dns --target f5.com
[test dns] Querying f5.com (A) via system resolver...
[test dns] Response: NOERROR (answer count: 2, latency P50: 10.31ms)
```

### 5.6 CLI & Go Codebase Hygiene
Run the entire unit and integration test suite across the repository:

```bash
$ go test ./...
ok  	github.com/JLCode-tech/azrbnkctl/internal/agent	(cached)
ok  	github.com/JLCode-tech/azrbnkctl/internal/azure/phases	17.655s
ok  	github.com/JLCode-tech/azrbnkctl/internal/cli	13.508s
ok  	github.com/JLCode-tech/azrbnkctl/internal/doctor	(cached)
ok  	github.com/JLCode-tech/azrbnkctl/internal/k8s	3.250s
ok  	github.com/JLCode-tech/azrbnkctl/internal/k8s/bnkscan	2.531s
ok  	github.com/JLCode-tech/azrbnkctl/internal/k8s/mcpsession	3.572s
ok  	github.com/JLCode-tech/azrbnkctl/internal/k8s/migrate	4.737s
ok  	github.com/JLCode-tech/azrbnkctl/internal/scenarios	4.462s
... (44 packages tested, 0 failures)
PASS
```

---

## 6. Testing Tooling & Lifecycle Command Reference

The `azrbnkctl` CLI provides a unified, 5-command core lifecycle:

```bash
# 1. Preflight Diagnostics
# Validates subscription, quota, Azure providers, and CLI tools
azrbnkctl doctor --subscription-id 00000000-0000-0000-0000-000000000000

# 2. Configuration Validation
# Validates cluster.yaml syntax, CIDR non-overlap, and manifest schema
azrbnkctl validate cluster.yaml

# 3. Dry-Run Simulation & Deployment
# Simulates resource creation offline
azrbnkctl up -f cluster.yaml --dry-run

# Provisions VNet, Subnets, AKS, Multus, Cert-Manager, FLO, and BNK
azrbnkctl up -f cluster.yaml

# 4. Day-2 Diagnostics & Status
# In-process kubectl proxy targeting cluster state
azrbnkctl k get pods -n f5-bnk
azrbnkctl k get infra,gatewaysettings,gateway,httproute -A

# Live backend doctor probe
azrbnkctl doctor --backend k8s

# Day-2 verification probes
azrbnkctl test egress -f cluster.yaml
azrbnkctl test dns --target f5.com

# 5. Teardown
# Cleans up all Custom Resources, deletes AKS, and tears down VNet
azrbnkctl down -f cluster.yaml
```

---

## 7. Lessons Learned & Recommendations for PoC Teams

1. **Avoid `eth0` in Data Plane CRDs**:
   Under no circumstances should an `Infra` or `GatewaySettings` network be named `eth0`. Doing so clobbers TMM's internal management socket (`xeth0`), severing gRPC communications on `:8750`. Always use semantic names like `external` and `internal`.
2. **Accelerated Networking on TMM VMSS**:
   Ensure the AKS VMSS node pool hosting TMM is deployed with Accelerated Networking enabled (`Standard_D8s_v3` or higher) to provide high-throughput SR-IOV Virtual Functions under the Linux NetVSC driver.
3. **Multus Token Retention**:
   Always deploy Multus with `--cleanup-config-on-exit=true` on AKS to ensure sandbox kubeconfig tokens mirror service account lifecycle.
4. **Workspace Kubeconfig Isolation**:
   When testing multiple cloud clusters, rely on `azrbnkctl k` or ensure tools inspect `artifacts/kubeconfig` before reading default `~/.kube/config`, avoiding cross-cluster context contamination.
