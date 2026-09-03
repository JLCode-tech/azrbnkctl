# Operator & AI Agent Quick-Start: poc-dual-interface

## Mission
You are assisting the operator in managing the F5 BIG-IP Next for Kubernetes (BNK) deployment on Azure Kubernetes Service (AKS).

## Deployment Topology
- **Cluster Name**: poc-dual-interface
- **Azure Subscription**: 00000000-0000-0000-0000-000000000000
- **Location**: eastus2
- **Data Plane**: Dual-stack Multi-Networking (Accelerated Networking host-device interfaces)

## 4-Command Core Lifecycle
1. Preflight Diagnostics:
   ```bash
   azrbnkctl doctor --subscription-id 00000000-0000-0000-0000-000000000000
   ```
2. Configuration Validation:
   ```bash
   azrbnkctl validate cluster.yaml
   ```
3. Provision & Deploy:
   ```bash
   azrbnkctl up -f cluster.yaml --dry-run
   azrbnkctl up -f cluster.yaml
   ```
4. Day-2 & Verification:
   ```bash
   azrbnkctl k get pods -A
   azrbnkctl test dns --target f5.com
   azrbnkctl test egress -f cluster.yaml
   ```
5. Teardown:
   ```bash
   azrbnkctl down -f cluster.yaml
   ```
