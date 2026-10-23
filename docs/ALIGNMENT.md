# *bnkctl Family Alignment & Feature Parity

`azrbnkctl` adheres strictly to the unified architectural conventions and UX patterns established across the `*bnkctl` tool family:

## Tool Family Map

| Tool | Cloud / Platform | Kubernetes Engine | Datapath Technology | Auth Mechanism |
| :--- | :--- | :--- | :--- | :--- |
| **`awsbnkctl`** | AWS | Amazon EKS | AWS Multinet / SR-IOV ENI | AWS IAM / STS |
| **`gkebnkctl`** | Google Cloud | Google GKE | GKE Multi-NIC / DPDK | Google Application Default Credentials (ADC) |
| **`roksbnkctl`** | IBM Cloud | Red Hat OpenShift (ROKS) | SRIOV Network Operator | IBM Cloud IAM API Key |
| **`ocibnkctl`** | Oracle Cloud | OCI Container Engine / K3s | Multus CNI / SR-IOV | OCI Instance / API Key Auth |
| **`azrbnkctl`** | Microsoft Azure | Azure Kubernetes Service (AKS) | Azure Accelerated Networking (SR-IOV / DPDK) | Azure `DefaultAzureCredential` |

---

## CLI Command Parity Matrix

All 24 top-level commands in `azrbnkctl` and their parity across the `*bnkctl` family:

| Subcommand | Purpose | `awsbnkctl` | `gkebnkctl` | `roksbnkctl` | `ocibnkctl` | `azrbnkctl` |
| :--- | :--- | :---: | :---: | :---: | :---: | :---: |
| `init` | Generate starter `cluster.yaml` intent file | ✓ | ✓ | ✓ | ✓ | ✓ |
| `validate` | Validate `cluster.yaml` schema, CIDRs, and version gates | ✓ | ✓ | ✓ | ✓ | ✓ |
| `up` | Provision cloud + AKS/K8s + BNK 2.4 stack (27 phases) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `down` | Destroy cluster and managed network resources | ✓ | ✓ | ✓ | ✓ | ✓ |
| `status` | Inspect Azure / AKS / BNK / Forge readiness | ✓ | ✓ | ✓ | ✓ | ✓ |
| `logs` | Stream TMM, controller, CWC, FLO, or governance logs | ✓ | ✓ | ✓ | ✓ | ✓ |
| `get` | Shorthand alias for `azrbnkctl k get` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `k` | Embedded `client-go` Kubernetes CLI (`get`, `apply`, `describe`, `delete`, `logs`, `exec`, `port-forward`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `doctor` | Preflight + live cluster readiness and heal diagnostics | ✓ | ✓ | ✓ | ✓ | ✓ |
| `test` | Data-plane verification (`connectivity`, `dns`, `egress`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `scenarios` | End-to-end BNK 2.4 traffic & AI gateway scenarios (`list`, `run`, `verify`, `clean`, `load`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `benchmark` | Run `aiperf` benchmarks from the in-VNet jumphost against BNK VIPs | ✓ | ✓ | — | — | ✓ |
| `bnk` | Day-2 BNK operations (`status`, `heal`, `upgrade`, `migrate-2.4`, `mcp-session`, `troubleshoot`) | ✓ | ✓ | — | — | ✓ |
| `forge` | BNK Forge integration (`register`, `status`, `unregister`, `cleanup`, `template`, `scan`, `telemetry`, `benchmark`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `bnkforge` | Backward-compatibility alias for `forge` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `topology` | Discover and print live BNK data-plane / node / VF topology | ✓ | ✓ | ✓ | ✓ | ✓ |
| `manifest` | Inspect and probe F5 FAR release manifests (`list`, `show`, `probe`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `journal` | Append-only operational journal (`add`, `list`, `show`, `path`, `clear`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `agent` | Launch AI coding assistant with cluster context (`claude`, `gemini`, `opencode`, `cursor`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `install` | Install `azrbnkctl` into `$PATH` (`/usr/local/bin` or `~/.local/bin`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `self` | Self-inspection and in-place GitHub Release binary upgrade (`info`, `update`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `version` | Print version, commit, and build date | ✓ | ✓ | ✓ | ✓ | ✓ |
| `completion` | Generate shell completion scripts (`bash`, `zsh`, `fish`, `powershell`) | ✓ | ✓ | ✓ | ✓ | ✓ |
| `help` | Built-in command help | ✓ | ✓ | ✓ | ✓ | ✓ |

---

## Technical Invariants
1. **Kubernetes Version Floor**: Every `*bnkctl` tool mandates Kubernetes version >= `1.32.0`.
2. **In-Process Dynamic SSA**: All Kubernetes operations run via `client-go` Server-Side Apply with FieldManager `<toolname>`.
3. **Pure Cloud SDKs**: No shell out to cloud CLIs (`az`, `aws`, `gcloud`, `ibmcloud`, `oci`) for cluster infrastructure lifecycle.
4. **Offline & Dry-Run Simulation**: Dry-run verification (`--dry-run` + auth bypass) guarantees testability in zero-credential CI environments.
