# Upgrade a BNK 2.3.x cluster to 2.4 in place

Two commands move a running BNK 2.3.x cluster on Azure AKS to 2.4 without rebuilding it:

1. `azrbnkctl bnk upgrade -f cluster.yaml` upgrades the F5 Lifecycle Operator
   and the CNEInstance.
2. `azrbnkctl bnk migrate-2.4 -f cluster.yaml --apply` translates the 2.3
   network and tenant CRs into the 2.4 model.

Run them in that order. The 2.4 CRDs (`Infra`, `GatewaySettings`,
`EgressGateway`, `SecPolicy`, `NetPolicy` under `gateway.k8s.f5.com`) arrive
with the 2.4 manifest, so the migration can only be applied after the upgrade.
The dry run works on either version.

## Step 1: `bnk upgrade`

```bash
azrbnkctl bnk upgrade -f clusters/lab/cluster.yaml --dry-run
azrbnkctl bnk upgrade -f clusters/lab/cluster.yaml
```

| Step | What happens |
|---|---|
| 0 | Multus: the thin plugin rewrites its kubeconfig on token rotation only with `--cleanup-config-on-exit=true`; without it the token expires and no new pod can start (`FailedCreatePodSandBox ... Multus ... Unauthorized`). The flag is added to the DaemonSet (which rolls it) before anything else; `azrbnkctl bnk heal` does the same on a cluster that stays on 2.3 (it runs the heal registry: `multus-token-watch`, `metrics-server`, `tmm-log-stream`, `pod-manager`, `cwc`, `dssm-probe`, `controller-endpointslices`, `controller-identity`, `tmm-k8s-routes`, `epp-namespaces`, `test-namespace`; `--only`, `--dry-run`), `azrbnkctl doctor --backend k8s` reports it as `multus kubeconfig token` |
| 1 | `helm upgrade f5-lifecycle-operator` to the chart paired with the target manifest (`v2.30.0-0.5.2` for `2.4.0`), values rendered from `cluster.yaml` as `up` renders them |
| 2 | JSON merge patch on the CNEInstance: `spec.manifestVersion`, controller env `USE_GATEWAY_SETTINGS=true`; `MAX_ACTIVE_TMM_REPLICAS=32` and the TMM env `ZEBOS_STATE=legacy` are added when absent; every other env entry is kept; the TMM env `TMM_K8S_ROUTES=<service range>` is added when absent (`--service-cidr`, else `10.0.0.0/16`) so DNS, dSSM (iRule `table`, persistence) and log forwarding stay reachable over eth0 once the `Infra` default route exists |
| 3 | Wait for the `Infra` CRD, then for FLO to render `USE_GATEWAY_SETTINGS=true` into the `f5-cne-controller` Deployment (3 min, `--timeout` for the rest). FLO 2.30 cannot update a CNEController created by FLO 2.21 (its payload carries `null` for `spec.crdUpdater.resources` and `f5CsmQkview`, which the 2.4 CRD rejects), so when the env does not appear the CNEController CR is deleted and FLO recreates it and the Deployment through its create path (`controllerRecreated` in `-o json`; about a minute without the controller, TMM keeps forwarding). Then the Deployment rollout, the CNEInstance conditions `CNEControllerAvailable` and `F5TmmAvailable`, and Ready `app=f5-tmm` pods |
| 3b | EndpointSlice RBAC: FLO 2.30 grants the controller only list/watch on `discovery.k8s.io` endpointslices; the 2.4 controller also gets them, and without `get` no pool ever has members (every connection to a VIP is reset). When a SubjectAccessReview for the controller SA denies `get`, the same ClusterRole + binding `azrbnkctl up` applies in phase 23b is applied and the controller restarted (`rbacApplied` in `-o json`) |
| 4 | Workload Identity: verified and aligned for the `f5-cne-controller` ServiceAccount federated credential |
| 5 | TMM log stream: the f5-fluentbit sidecar only forwards, so the `@type stdout` store in `f5-toda-fluentd-custom` is enabled and the fluentd pod bounced once; `azrbnkctl logs tmm --governance` and the governance collector read that stream (`logStreamEnabled` in `-o json`) |

`--manifest-version` defaults to `2.4.0`. The string F5's docs print,
`2.4.0-3.3175.0+0.0.380`, is accepted and rewritten to `2.4.0`: that is the tag
on `repo.f5.com` and the name FLO matches (`bigip-k8s-manifest-2.4.0.yaml`).
`--flo-version` overrides the paired chart. The command is idempotent: a
release already on the chart and a CNEInstance already on the version are
reported and skipped.

`-o json` prints the result (`floFrom`, `floTo`, `floUpgraded`, `manifestFrom`,
`manifestTo`, `patched`, `tmmReady`, `tmmTotal`, `controllerReady`,
`controllerRecreated`, `rbacApplied`, `logStreamEnabled`, `multus`, `readiness`). The
`readiness` block is the shared bnkscan verdict `status`, `doctor` and
`forge scan` print; right after an upgrade it reports `mixed` with no Infra CR
and points at `bnk migrate-2.4`.

## Step 2: `bnk migrate-2.4`

```bash
azrbnkctl bnk migrate-2.4 -f clusters/lab/cluster.yaml --dry-run > plan.yaml
azrbnkctl bnk migrate-2.4 -f clusters/lab/cluster.yaml --apply
```

The dry run prints the 2.4 manifests on stdout and a report on stderr: the
inventory, the objects the plan holds, and warnings for every 2.3 value the 2.4
model has no field for. Read the warnings before applying.

| 2.3 resource | 2.4 result |
|---|---|
| `F5SPKVlan` | `Infra` network (`type: vlan`, same name, tag, mtu), a network attachment resolved from CNEInstance `networkAttachments` (interface `1.N` is the Nth NAD), and a self-IP IPAM pool: the /27 block around the 2.3 self IP (`--single-self-ip` keeps the exact address) |
| `F5SPKStaticRoute` (type `gateway`) | `Infra` `staticRoutes` entry (`destination/prefixLen`, `nextHop`) |
| no `F5SPKStaticRoute` | a default route `0.0.0.0/0` via the external VLAN gateway, as `up` renders it; the report warns |
| `Vrf` | `Infra` `vrfs` entry |
| `Vxlan` | `Infra` network (`type: vxlan`) with a VTEP pool |
| `F5SPKSnatpool` | `Infra` IPAM pool `<name>-snat`, referenced by `GatewaySettings.sourceNATPools` |
| `F5SPKEgress` | `Infra` `egressDefaults` (tunnel subnet and VLAN), one `GatewaySettings` with `egressConfigs` and one `EgressGateway` per `pseudoCNIConfig.namespaces` entry; `snatType` becomes `sourceNATConfig` (`Automap`, `None`, `Pool`); `firewallEnforcedPolicy` becomes a `SecPolicy` targeting the `EgressGateway` |
| none | A cluster without any `F5SPKEgress` still gets `Infra` `egressDefaults` (`192.168.0.0/16`, port 4789, the tunnel VLAN: internal where one exists, else external); without the block the controller defaults the tunnel subnet to `10.0.0.0/16`, which overlaps the VNet self-IP pools and leaves the Infra `Programmed=False` (`EgressDefaultsSubnetConflict`) |
| `F5BnkGateway` + `Gateway` | a listener IPAM pool in `Infra`, a `GatewaySettings` per BNK Gateway (`ipamRefs`, external `networkRefs`, `Automap`), and a server-side-apply patch adding `spec.infrastructure.parametersRef` to the Gateway; a Gateway without an `F5BnkGateway` gets a pool from its static address |
| `BNKSecPolicy`, `BNKNetPolicy` | `SecPolicy`, `NetPolicy` in `gateway.k8s.f5.com/v1alpha1`, same name, namespace, labels and spec |

Gateways on a non-F5 `GatewayClass` are left alone. `--gateway-class` names
the class the `EgressGateway`s use when the plan cannot derive it.

`--apply` server-side-applies the objects in order (Infra, GatewaySettings,
Gateway patches, EgressGateways, policies) and waits for the Infra CR to report
`Programmed=True` (`--wait`, default 5 minutes). The 2.3 CRs stay in place: the
controller ignores them once `USE_GATEWAY_SETTINGS` is set, and they are the
rollback. Delete them by hand once the 2.4 cluster has been verified.

## After the migration

- The self IPs the F5 IPAM controller allocates from the new pools are written
  to the `IPAM` CRs in the CNE namespace. On Azure Accelerated Networking, ensure
  IP configurations correspond to the secondary IP assignments on the network interface.
- BGP: the ZebOS interface names are the Infra network names, which the
  migration keeps, so an existing `f5-tmm-dynamic-routing-template` ConfigMap
  stays valid. `allowed_services` on the 2.3 VLAN has no 2.4 field and is not
  needed.
- Firewall policies moved to a tenant `SecPolicy` need their `F5BigFwPolicy`
  and address lists in that tenant namespace.
