# Command reference

Every command accepts `--help`. Global persistent flags on every command:
`-o/--output text|json`, `-v/--verbose`, `-q/--quiet`, `--no-color`.

## Lifecycle

| Command | Description |
|---|---|
| `init [directory]` | Scaffold a new cluster workspace (`cluster.yaml`, `keys/`, `artifacts/`, `AGENTS.md`, `.env`); flags: `--subscription-id`, `--name`, `--location`, `--pattern`, `--vnet-cidr`, `--ext-cidr`, `--int-cidr`, `-f/--force` |
| `validate <path>` | Parse and validate a `cluster.yaml` (no Azure API calls) |
| `up -f <config>` | Provision the Azure VNet, NAT Gateway, AKS cluster, TMM node pool, and BNK 2.4 stack across 27 phases (`--dry-run` prints the execution plan with zero Azure mutations) |
| `down -f <config>` | Destroy the AKS cluster and managed network resources in reverse dependency order (`--dry-run`, `--yes`, `--delete-rg`, `--keep-rg`) |
| `status` | Summary of the workspace: cluster, Azure state (`.azrbnkctl/<cluster>/state.env`), BNK API generation, `Infra` / `Gateway` / controller readiness (`bnkscan`), and Forge link (`-f/--config`) |
| `doctor` | Verify host prerequisites, Azure credentials, subscription, resource providers, and cluster readiness (`--subscription-id`, `--location`, `--strict`, `--backend k8s`) |
| `topology -f <config>` | Render the VNet, subnet, AKS agent pool, TMM data-plane, and jumphost topology (`--format ascii\|mermaid\|dot\|json`, `--live`, `--static`, `--kubeconfig`) |
| `kubeconfig -f <config>` | Fetch and export AKS cluster credentials (`--export`, `--write-global`, `--restore-global`, `--out`) |
| `version` | Print version, commit, and build date |

By default, `azrbnkctl down -f <cluster.yaml>` preserves the Azure Resource Group to safeguard shared scopes and RBAC assignments. Pass `--delete-rg` only when you want to delete the entire Resource Group as well.

## Validation and scenarios

| Command | Description |
|---|---|
| `test connectivity` | Probe TCP/UDP network reachability and round-trip time (`--target`, `--proto tcp\|udp`, `--count`, `--timeout`, `--json`, `--dry-run`) |
| `test dns` | Probe DNS resolution and calculate latency percentiles (Min/P50/P95/P99/Max) (`--target`, `--type`, `--server`, `--iterations`, `--timeout`, `--edns-subnet`, `--json`, `--dry-run`) |
| `test egress` | Run live Phase 50 VXLAN + TMM Automap SNAT egress validation on a deployed AKS BNK cluster (`-f/--file`, `--dry-run`) |
| `test traffic` | Drive HTTP curl probes through TMM from the Azure jumphost VM or in-cluster probe pod (`-f/--config`, `--vip`, `--iterations`, `--timeout`, `--instance`, `--region`, `--user`, `--host`, `--dry-run`) |
| `scenarios list` | Print all 15 registered validation scenarios with their rating (`Green` / `Amber`) and title |
| `scenarios run <name>` | Run a validation scenario (or `--all` in topologically sorted dependency order); `-f/--config`, `--vip`, `--synthetic` (runs `ai-inference-e2e` without GPU nodes), `--dry-run` |
| `scenarios clean <name>` | Invoke a scenario's Cleanup hook (or `--all` in reverse dependency order); `-f/--config` |

## Kubernetes and BNK runtime

| Command | Description |
|---|---|
| `k apply` | Server-side apply YAML/JSON manifests, directories, or kustomize bases (`-f`, `-k`, `--dry-run`, `--force-conflicts`) |
| `k delete` | Delete resources by file, name, or label selector (`-f`, `-n`, `-l`, `--all`, `--wait`, `--grace-period`) |
| `k describe` | Detailed resource info (events, conditions, related objects) |
| `k exec` | Exec into a pod via SPDY (`-n`, `-c`, `-i`, `-t`) |
| `k get` | List or show one or more resources (`-n`, `-A`, `-l`, `-o yaml\|json\|wide\|name\|jsonpath=…`) |
| `k logs` | Stream pod logs (`-n`, `-c`, `-f`, `--tail`, `--since`, `--previous`) |
| `k port-forward` | Forward local ports to a pod or `svc/<name>` via SPDY |
| `get <resource> [name]` | Top-level alias of `k get` (`-n`, `-A`, `-l`, `-o yaml\|json\|wide\|name\|jsonpath=…`) |
| `logs <component>` | Tail logs for a BNK component (`flo`, `cwc`, `cis`, `cne`, `cert-manager`, `cneinstance`, `bgp`, `tmm`); `-f`, `-n`, `-c`, `--since`, `--tail`, `--previous`; `--governance` / `--mcp` format `BNKGOV` records as `[GOV] <status> <rpc_method> tool= session= latency= action=` |
| `bnk heal` | Detect and repair the 11 cluster plumbing checks (`multus-token-watch`, `metrics-server`, `tmm-log-stream`, `pod-manager`, `cwc`, `dssm-probe`, `controller-endpointslices`, `controller-identity`, `tmm-k8s-routes`, `epp-namespaces`, `test-namespace`); `-f/--config`, `--kubeconfig`, `--only`, `--dry-run`, `-o json` |
| `bnk resync [httproute]` | Force the F5 `cne-controller` to re-resolve stale TMM pool members (`-n`, `--all-in-ns`, `--gateway-class`, `--dry-run`, `--watch`, `--debounce`, `-f/--config`, `--kubeconfig`) |
| `bnk migrate-2.4` | Translate 2.3.x CRs of a running cluster into the 2.4 `Infra` / `GatewaySettings` model (`--dry-run`, `--apply`, `-f/--config`, `--kubeconfig`, `-n`, `--gateway-class`, `--infra-name`, `--single-self-ip`, `--wait`) |
| `bnk upgrade -f <config>` | In-place upgrade: Helm upgrade FLO, patch the `CNEInstance` (`manifestVersion`, `USE_GATEWAY_SETTINGS`), verify Azure Workload Identity, and watch controller + TMM rollout (`--manifest-version`, `--flo-version`, `--service-cidr`, `--instance`, `-n`, `--kubeconfig`, `--timeout`, `--dry-run`) |
| `bnk mcp-session` | Render or `--apply` an `F5BigPersistenceProfile` (`MODEL_CONTEXT_PROTOCOL` or `AGENT2AGENT`), its passphrase `Secret`, and the `NetPolicy` per listener that pins MCP sessions to one backend (`--name`, `--type`, `--gateway`, `--listener`, `--irule`, `--passphrase-env`, `--secret`, `--net-policy-name`, `--apply`, `--wait`) |
| `manifest probe [version]` | Inspect a BNK release manifest (Bill of Materials) from `repo.f5.com` or a local FAR archive without a cluster (`--far <path>`, `--all`) |

## AI benchmarking and BNK Forge

| Command | Description |
|---|---|
| `benchmark` | Runs the default `benchmark run` workflow |
| `benchmark setup` | Prepare the Azure jumphost (`aiperf >= 0.10.0`) and register the SSH AccessMethod, BenchmarkAgent, and BenchmarkTarget in Forge (`-f/--config`, `--preflight`, `--daemon`) |
| `benchmark run` | Drive an `aiperf` run, smoke preset (`--scenarios`), native Forge scenario sweep (`--scenario`), or multi-proxy shootout (`--proxies`); `--prefix-prompt-length`, `--num-prefix-prompts`, `--random-seed` for shared-prefix workloads; `--metrics-pod-selector` / `--metrics-url` scrape vLLM or EPP metrics for prefix-cache hit rate; `--genai-out`, `--synthetic`, `--reset-cache` |
| `benchmark list` | List native Forge scenarios and built-in smoke presets (`-o json`) |
| `benchmark status` | Check the benchmark environment, Azure jumphost readiness, `aiperf` version, and Forge linkage (`-f/--config`, `-w/--workspace`) |
| `benchmark daemon` | Run the persistent Forge benchmark agent daemon over WebSocket with agent-bound token authentication (`--forge-agent-token`, `--heartbeat-interval`) |
| `forge register` | Register the AKS cluster with BNK Forge via MCP (preferred) or REST (`-f/--file`, `--cluster-name`, `--kubeconfig`, `--project-name`, `--template-id`, `--scan`, `--rest`) |
| `forge status` | Show the cluster's Forge registration state (`-f/--file`, `--json`) |
| `forge unregister` | Remove the cluster's Forge registration (`--purge` also deletes the project shell) |
| `forge cleanup` | Delete benchmark artifacts associated with a cluster in Forge (`--workspace`, `--dry-run`) |
| `forge template {list,create}` | List or create Azure Cloud Credential Templates in BNK Forge (`--name`, `--subscription-id`, `--tenant-id`, `--credentials`, `--default`, `--json`) |
| `forge scan` | Index the cluster's BNK 2.4 resources, check readiness (`Infra`, `Gateway`, controller), and discover MCP endpoints (`--probe`, `--bearer-env`, `--register-targets`, `--remote`, `-o json`) |
| `forge telemetry` | Validate the MCP governance records in Loki against Forge's LLM Observability schema (`--loki-url`, `--query`, `--since`, `--limit`, `--file`, `--show`, `-o json`) |
| `forge benchmark` | Alias for `benchmark run` |
| `bnkforge {register,status,unregister}` | Backward-compatibility alias for BNK Forge topology daemon registration (`--url`, `-f/--file`, `--json`) |

Every `forge` command accepts `-f/--file <cluster.yaml>`, `--mcp-url`, `--rest-url`, `--username`, and `--password`. See [`BENCHMARKS.md`](BENCHMARKS.md) for benchmark presets and proxy shootouts, and [`FORGE_INTEGRATION.md`](FORGE_INTEGRATION.md) for Forge integration details.

## Agentic workflow

| Command | Description |
|---|---|
| `agent [gemini\|claude\|opencode\|cursor\|all]` | Generate tailored AI pair-programming assistant instructions (`AGENTS.md`, `CLAUDE.md`, `.cursorrules`) from `cluster.yaml` (`-f/--file`, `--force`) |
| `journal add <message>` | Append a timestamped audit entry to `artifacts/journal.jsonl` (`--actor`, `--action`, `--tag`, `--file`) |
| `journal list` | Display all entries recorded in the workspace journal (`--json`, `--file`) |

## Installation and maintenance

| Command | Description |
|---|---|
| `install` | Copy the running `azrbnkctl` binary into a directory on `PATH` (`--dir`, `--force`) |
| `self check` | Check GitHub Releases for newer versions of `azrbnkctl` (`--repo`) |
| `self update` | Download and SHA-256 verify the latest release matching the host OS/arch (`--repo`) |
| `completion <shell>` | Generate the shell completion script (`bash`, `zsh`, `fish`, `powershell`) |
| `help [command]` | Help about any command |

## Environment variables

| Variable | Effect |
|---|---|
| `AZRBNKCTL_SKIP_AUTH=1` | Skip Azure credential resolution; used together with `--dry-run` on `up` / `down` |
| `AZURE_SUBSCRIPTION_ID` / `ARM_SUBSCRIPTION_ID` | Default Azure Subscription ID when `metadata.subscriptionId` is not set in `cluster.yaml` |
| `AZURE_TENANT_ID` / `AZURE_CLIENT_ID` / `AZURE_CLIENT_SECRET` | Standard Azure SDK service principal credentials consumed by `DefaultAzureCredential` |
| `KUBECONFIG` | Explicit kubeconfig path override when `--kubeconfig` is not passed |
| `AZRBNKCTL_FORGE_URL` | BNK Forge REST base URL (overrides `forge.url`; default `http://localhost:8000`) |
| `AZRBNKCTL_FORGE_MCP_URL` | BNK Forge MCP endpoint (overrides `forge.mcpUrl`; default `http://localhost:8081/mcp/`) |
| `AZRBNKCTL_FORGE_USERNAME` / `AZRBNKCTL_FORGE_PASSWORD` | Forge REST credentials; the password is never stored in plain text in production configs |
| `AZRBNKCTL_FORGE_AGENT_TOKEN` | Forge benchmark agent JWT (role `agent` with `agent_id` claim; overrides `forge.agentToken` and cached token) |
| `AZRBNKCTL_PROBE_IN_CLUSTER=1` | Route scenario and traffic probes through an in-cluster probe pod instead of jumphost SSH |
| `AZRBNKCTL_PROBE_POD` / `AZRBNKCTL_PROBE_NAMESPACE` / `AZRBNKCTL_PROBE_CONTAINER` | Customize the in-cluster probe pod target (set `AZRBNKCTL_PROBE_POD=auto` to auto-select `f5-tmm`) |
| `AZRBNKCTL_FORCE_SSH=1` | Force jumphost SSH execution even if an in-cluster probe pod is running |
| `HF_TOKEN` | Hugging Face token for gated models in `ai-inference-e2e` |
| `GITHUB_TOKEN` / `GH_TOKEN` | Optional GitHub token used by `self check` and `self update` to avoid API rate limits |
