# local-zone — Azure Local & Edge Reference Manifests

This directory provides F5 BIG-IP Next for Kubernetes (BNK) 2.4 reference custom resources designed for **Azure Local (formerly Azure Stack HCI / Azure Arc)**, **Azure Edge Zones**, and regional edge deployments.

No `cluster.yaml` is required in this directory; these manifests are applied to an existing cluster (such as [`examples/local-zone-eastus2/`](../local-zone-eastus2/) or [`examples/dual-interface/`](../dual-interface/)).

Operational and validation findings are documented in [`docs/local-zones-validation.md`](../../docs/local-zones-validation.md).

---

## Reference Manifests

| File | Protocol & Port | Target VIP | Description |
|---|---|:---:|---|
| `manifests/http2.yaml` | HTTP/2 (`h2c` on :80) | `10.0.17.123` | End-to-end HTTP/2 multiplexing with `GatewaySettings`, backend, `Gateway`, and `HTTPRoute`. |
| `manifests/diameter.yaml` | Diameter (TCP :3868) | `10.0.17.122` | Telecom Diameter RFC 6733 L4 route with `GatewaySettings`, netshoot backend, and `L4Route`. |
| `manifests/sctp.yaml` | SCTP (:9000) | `10.0.17.121` | Telco SCTP echo backend, Gateway, and `L4Route`. |
| `manifests/egress.yaml` | Egress SNAT | – | `GatewaySettings` + `EgressGateway` capturing the three test namespaces with Automap SNAT on `int-vlan-infra`. |

---

## Applying Manifests to a Cluster

Apply the HTTP/2 scenario to your deployed cluster:
```bash
kubectl apply -f examples/local-zone/manifests/http2.yaml
```

Verify Gateway programming and HTTPRoute attachment:
```bash
kubectl -n http2-scenario get gateway,httproute
```

Test from your edge jumphost or client:
```bash
curl --http2-prior-knowledge http://10.0.17.123/
```

Clean up:
```bash
kubectl delete namespace http2-scenario
```
