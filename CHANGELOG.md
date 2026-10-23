# Changelog

All notable changes to `azrbnkctl` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.2.0](https://github.com/JLCode-tech/azrbnkctl/compare/v1.1.0...v1.2.0) (2026-10-23)


### Features

* align F5 BNK 2.4 namespaces, probe pod fallback, and CLI observability ([#32](https://github.com/JLCode-tech/azrbnkctl/issues/32)) ([15ae13a](https://github.com/JLCode-tech/azrbnkctl/commit/15ae13a1fd3a488783975a443c11fc4c29f38766))
* **azure:** sync unified bnkctl 2.4.1 AI Gateway, Forge governance, and public-ready docs ([895d795](https://github.com/JLCode-tech/azrbnkctl/commit/895d79575665b1a0c7d266d85e9e9e004add75e8))
* **azure:** sync unified bnkctl 2.4.1 AI Gateway, Forge governance, and public-ready docs ([3e076ca](https://github.com/JLCode-tech/azrbnkctl/commit/3e076cae02574bf337ab7daa6175385584734c39))
* BNK 2.4 Infra/EgressGateway reconciliation, JWKS rotation, RBAC & example parity ([c7fd742](https://github.com/JLCode-tech/azrbnkctl/commit/c7fd742adcb4cd1ea138e7ec8fc781c3a6310fd7))
* BNK 2.4 Infra/EgressGateway reconciliation, JWKS rotation, RBAC & example parity ([f50d380](https://github.com/JLCode-tech/azrbnkctl/commit/f50d38072b2fc211ce7f40d30c435dfb698d3734))
* **cli,ci,docs:** fix self update archive extraction, add install & scenarios --all, and align CI/docs ([#36](https://github.com/JLCode-tech/azrbnkctl/issues/36)) ([224766c](https://github.com/JLCode-tech/azrbnkctl/commit/224766c326e2490e8a9b9dabfe06caad1fa76abe))
* **k8s,jumphost:** internalize k subcommands and harden bastion jumphost ([#29](https://github.com/JLCode-tech/azrbnkctl/issues/29)) ([2411bf3](https://github.com/JLCode-tech/azrbnkctl/commit/2411bf3680dff479e65fdec8faeb33a6074a492d))
* **lifecycle:** preserve resource group by default during down and require --delete-rg ([3818919](https://github.com/JLCode-tech/azrbnkctl/commit/381891956c8e4b6ecf45e667c33150e5d2c747d9))
* **lifecycle:** preserve resource group by default during down and require --delete-rg ([993c747](https://github.com/JLCode-tech/azrbnkctl/commit/993c747fddd093110305e44adb0dfa18fe43beb6))
* **lifecycle:** preserve resource group by default during down and require --delete-rg ([#31](https://github.com/JLCode-tech/azrbnkctl/issues/31)) ([3818919](https://github.com/JLCode-tech/azrbnkctl/commit/381891956c8e4b6ecf45e667c33150e5d2c747d9))
* **parity:** backport P0/P1 cross-cloud parity fixes to azrbnkctl ([7b15679](https://github.com/JLCode-tech/azrbnkctl/commit/7b156796e32a4fcd32aea331da152adcd54a8153))
* **parity:** backport P0/P1 cross-cloud parity fixes to azrbnkctl ([be38340](https://github.com/JLCode-tech/azrbnkctl/commit/be38340b7ad86edd0e57e1fd9712e03d4c8126e3))
* **scenarios:** add scenarios, Azure LocalZone engineering guide, and full examples suite ([ee8473b](https://github.com/JLCode-tech/azrbnkctl/commit/ee8473bac8ce261fe88d61b348bb4457a3d3093a))
* **scenarios:** add scenarios, Azure LocalZone engineering guide, and full examples suite ([3952496](https://github.com/JLCode-tech/azrbnkctl/commit/3952496a2529595d2829861cc2de4c66202c44ee))


### Bug Fixes

* **ci:** add teardowns to cspell wordlist for doc validation ([4989108](https://github.com/JLCode-tech/azrbnkctl/commit/49891085da3d86b2306e90be23d2093f2aa97180))
* **down,forge:** propagate non-404 Azure delete errors and support wrapped Forge REST lists ([#35](https://github.com/JLCode-tech/azrbnkctl/issues/35)) ([db620fb](https://github.com/JLCode-tech/azrbnkctl/commit/db620fb1fb32f648e6cfc4850fcae349055efe8c))
* **parity:** remove AKS cluster reconcile from bnk upgrade and align aiperf agentx-mvp with awsbnkctl ([4374309](https://github.com/JLCode-tech/azrbnkctl/commit/43743094132b6c2ea99438a9937b8274a97148bf))
* port runtime parity and lifecycle fixes across phases, scenarios, and CLI ([4a5f33b](https://github.com/JLCode-tech/azrbnkctl/commit/4a5f33bc0dd1cc55d6e1870c5ad46c21c2219a62))
* **scenarios,azure:** resolve TMM dataplane probe target, CWC namespace discovery, and activation sync ([5bd66dd](https://github.com/JLCode-tech/azrbnkctl/commit/5bd66dd0166c18cdfc0813509c3372e21e774b8e))
* **security:** restrict artifacts directory permission to 0750 for gosec G301 ([233ec5a](https://github.com/JLCode-tech/azrbnkctl/commit/233ec5a7df1b31d08305be5bc2b52cf6b67838cc))

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
