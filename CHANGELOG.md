# Changelog

All notable changes to `azrbnkctl` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.1.0](https://github.com/JLCode-tech/azrbnkctl/compare/v1.0.0...v1.1.0) (2026-10-07)


### Features

* **benchmark:** port benchmark subsystem and bidirectional agent daemon ([e7d340f](https://github.com/JLCode-tech/azrbnkctl/commit/e7d340f569a0211e31f8bf9a9bd118b8763e23b2))
* **parity:** port BNK 2.4 migration, heal registry, agentic forge & scenarios from awsbnkctl ([7fc000d](https://github.com/JLCode-tech/azrbnkctl/commit/7fc000da80ba30b1075d923144ed790c28ed59a7))


### Bug Fixes

* **ci:** allowlist test fixture in .gitleaksignore and sanitize test string ([526e8b4](https://github.com/JLCode-tech/azrbnkctl/commit/526e8b4d3aae704a8c556801329cc46e9e1e114e))
* **ci:** bump dependencies for govulncheck and add missing words to cspell ([162f74f](https://github.com/JLCode-tech/azrbnkctl/commit/162f74fabd0cdf99c6e9d1f77d2765bff091b638))
* **dataplane:** configure TMM socket driver and eth0 vlan mapping for Azure AKS ([6995110](https://github.com/JLCode-tech/azrbnkctl/commit/699511089d031ca2fbb23ed6387216c80344f1b0))
* **dataplane:** configure TMM socket driver and eth0 vlan mapping for Azure AKS ([#12](https://github.com/JLCode-tech/azrbnkctl/issues/12)) ([2b5f711](https://github.com/JLCode-tech/azrbnkctl/commit/2b5f711718704e7c27b49135dd6a9c3d0096d2e4))

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
