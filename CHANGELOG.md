# Changelog

All notable changes to `azrbnkctl` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 1.0.0 (2026-09-03)


### Features

* initial azrbnkctl release and core lifecycle engine ([#2](https://github.com/JLCode-tech/azrbnkctl/issues/2)) ([7d2421f](https://github.com/JLCode-tech/azrbnkctl/commit/7d2421f62c8ba433cb8019fdc34171350204fa9b))

## [v0.9.0-rc1] — 2026-09-04

### Added
- Single-binary provisioner and lifecycle manager for F5 BIG-IP Next for Kubernetes (BNK) on Azure AKS.
- 18 deterministic, ordered provisioning phases with Azure resource tagging and idempotency.
- Native Azure Accelerated Networking VF PCI address discovery (`4567:00:02.0`).
- Automated Node Prep DaemonSet for 4Gi HugePages allocation and kernel core pattern setup.
- Full reconciliation for F5 CNE / SPK CRDs (`f5-spk-vlans.k8s.f5net.com`, `licenses.k8s.f5net.com`, `f5-spk-egresses.k8s.f5net.com/v3`).
- Built-in embedded Kubernetes engine (`azrbnkctl k ...`) with zero host `kubectl` dependency.
- Day-2 diagnostic suite (`azrbnkctl test {dns,connectivity,egress}`).
- Built-in BNK Forge cluster registration and project link persistence.
- Agentic workflow and MCP integration (`azrbnkctl agent`, `azrbnkctl journal`, `azrbnkctl mcp`).
