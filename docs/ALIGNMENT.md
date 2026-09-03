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

| Subcommand | `awsbnkctl` | `gkebnkctl` | `roksbnkctl` | `ocibnkctl` | `azrbnkctl` |
| :--- | :---: | :---: | :---: | :---: | :---: |
| `init` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `validate` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `doctor` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `up` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `down` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `kubeconfig` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `k` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `test dns` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `test connectivity`| ✓ | ✓ | ✓ | ✓ | ✓ |
| `test egress` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `manifest probe` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `journal` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `agent` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `bnkforge` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `self` | ✓ | ✓ | ✓ | ✓ | ✓ |
| `version` | ✓ | ✓ | ✓ | ✓ | ✓ |

---

## Technical Invariants
1. **Kubernetes Version Floor**: Every `*bnkctl` tool mandates Kubernetes version >= `1.32.0`.
2. **In-Process Dynamic SSA**: All Kubernetes operations run via `client-go` Server-Side Apply with FieldManager `<toolname>`.
3. **Pure Cloud SDKs**: No shell out to cloud CLIs (`az`, `aws`, `gcloud`, `ibmcloud`, `oci`) for cluster infrastructure lifecycle.
4. **Offline & Dry-Run Simulation**: Dry-run verification (`--dry-run` + auth bypass) guarantees testability in zero-credential CI environments.
