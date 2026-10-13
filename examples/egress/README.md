# egress — Transparent Outbound Steering & Egress Firewall

| Attribute | Specification |
|---|---|
| **Cluster Name** | `bnk-egress` |
| **Region / Pattern** | `eastus2` / `external-only` |
| **Footprint** | 2 × `Standard_D4s_v5` (system), 1 × `Standard_D8s_v5` (TMM), 1 Azure NAT Gateway |
| **Estimated Cost** | ~$2.50 / hour |

Apply one `EgressGateway` (with its `GatewaySettings`) and a pod's outbound traffic flips to leave through TMM with AUTOMAP SNAT and an egress security policy. Delete it and the pod returns to the node's standard default route. The application pod configuration is never modified.

---

## How It Works

1. **`GatewaySettings`**: Declares an `egressConfigs` entry configuring Automap SNAT on the external VLAN network (`ext-vlan-infra`).
2. **`EgressGateway`**: References the `GatewaySettings` and selects the captured namespace (`bnk-egress-demo`).
3. **Firewall Policy**: `F5BigFwPolicy` attached to the `EgressGateway` via a `SecPolicy`. Drops `1.1.1.1` and allows/logs all other outbound destinations.

Captured traffic is SNATed to TMM's external self-IP and leaves Azure via the deterministic public NAT Gateway. Intra-cluster pod-to-pod and pod-to-VNet traffic remains on the Azure CNI overlay.

---

## Manifest Files

| File | Purpose |
|---|---|
| `cluster.yaml` | The external-only cluster configuration |
| `workload.yaml` | The captured `agent` pod (`bnk-egress-demo`) and uncaptured `control` pod (`bnk-egress-control`) |
| `firewall-policy.yaml` | `F5BigCneAddresslist` + `F5BigFwPolicy` + `SecPolicy` attaching them to the `EgressGateway` |
| `egress-toggle.yaml` | The `GatewaySettings` + `EgressGateway`: apply = BNK ON, delete = BNK OFF |
| `probe.sh` / `watch.sh` | What the pod sees: external source IP and whether `1.1.1.1` is reachable |

---

## Running the Demo

### 1. Provision Cluster
```bash
./azrbnkctl up -f examples/egress/cluster.yaml
export KUBECONFIG=.azrbnkctl/bnk-egress/kubeconfig
```

### 2. Deploy Workloads & Firewall Policy
```bash
kubectl apply -f examples/egress/workload.yaml
kubectl apply -f examples/egress/firewall-policy.yaml
kubectl -n bnk-egress-demo rollout status deploy/agent --timeout=120s
```

### 3. Open Terminal 1: Pod Live View
```bash
kubectl exec -it -n bnk-egress-demo deploy/agent -c nginx -- sh /demo/watch.sh
```

### 4. Open Terminal 2: The Egress Toggle
```bash
# Flip BNK Egress ON:
kubectl apply -f examples/egress/egress-toggle.yaml

# Flip BNK Egress OFF:
kubectl delete -n bnk-egress-demo egressgateway bnk-egress-demo
```

### What You Observe
- **BNK ON**: The internet sees the Azure NAT Gateway public IP (traffic left via TMM), and `1.1.1.1` is **BLOCKED** by the `SecPolicy`.
- **BNK OFF**: The internet sees the worker node's public IP or standard egress, and `1.1.1.1` is reachable.
- The control pod in `bnk-egress-control` is never captured, verifying unaffected tenant isolation side-by-side.

---

## Teardown

```bash
kubectl delete -n bnk-egress-demo egressgateway/bnk-egress-demo gatewaysettings/bnk-egress-demo
kubectl delete -f examples/egress/firewall-policy.yaml
kubectl delete -f examples/egress/workload.yaml
./azrbnkctl down -f examples/egress/cluster.yaml
```
