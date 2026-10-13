#!/usr/bin/env bash
# bringup.sh — provisions the shootout frontends on Azure AKS.
set -euo pipefail

CFG="${CFG:-examples/demo-ai/cluster.yaml}"
CLUSTER="bnk-demo-ai"
VIP="10.0.17.130"
export KUBECONFIG="${KUBECONFIG:-.azrbnkctl/${CLUSTER}/kubeconfig}"

echo "==> Configuring Shootout Namespace and Backends"
kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: shootout
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inference-backend
  namespace: shootout
spec:
  replicas: 1
  selector:
    matchLabels:
      app: inference-backend
  template:
    metadata:
      labels:
        app: inference-backend
    spec:
      containers:
      - name: sim
        image: ghcr.io/llm-d/llm-d-inference-sim:v0.11.2
        ports:
        - containerPort: 8000
---
apiVersion: v1
kind: Service
metadata:
  name: inference-backend
  namespace: shootout
spec:
  selector:
    app: inference-backend
  ports:
  - port: 8000
    targetPort: 8000
---
apiVersion: gateway.k8s.f5.com/v1alpha1
kind: GatewaySettings
metadata:
  name: shootout-settings
  namespace: shootout
spec:
  ingressConfig:
    defaultListenerNetwork:
      ipamRefs:
      - name: listener-pool
      networkRefs:
      - name: ext-vlan-infra
      sourceNATConfig:
        type: Automap
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: shootout-gateway
  namespace: shootout
spec:
  gatewayClassName: bnk-demo-ai-gatewayclass
  infrastructure:
    parametersRef:
      group: gateway.k8s.f5.com
      kind: GatewaySettings
      name: shootout-settings
  addresses:
  - type: IPAddress
    value: ${VIP}
  listeners:
  - name: http
    protocol: HTTP
    port: 80
    allowedRoutes:
      namespaces:
        from: Same
---
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: shootout-route
  namespace: shootout
spec:
  parentRefs:
  - name: shootout-gateway
    sectionName: http
  rules:
  - backendRefs:
    - name: inference-backend
      port: 8000
EOF

echo "==> Shootout bring-up complete. BNK VIP is ${VIP}:80"
