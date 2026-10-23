package cli

// Package cli — benchmark subcommand.
//
// azrbnkctl forge benchmark
//   Runs aiperf FROM the in-network jumphost (the only host that can reach
//   the BNK VIP on the external VLAN) against the VIP, then pushes the
//   result to a running forge instance.
//
// Prerequisites on the jumphost:
//   pip install aiperf          # one-time; or pass --ensure-aiperf
//
// Example:
//   azrbnkctl forge benchmark \
//     --region australiaeast \
//     --instance-id jumphost-vm \
//     --source-ip 10.0.11.50 \
//     --vip 10.0.10.100 \
//     --model meta-llama/Llama-3.1-8B-Instruct \
//     --forge-rest-url http://localhost:8000 \
//     --run-label nightly-ci
//
// Multi-scenario example:
//   azrbnkctl forge benchmark ... --scenarios latency,throughput
//   azrbnkctl forge benchmark ... --scenarios all

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/forge"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/jumphost"
)

// discoverProxiesFn is the injectable seam for forge.DiscoverProxies.
var discoverProxiesFn = forge.DiscoverProxies

// listProxyDeploymentsFn is the injectable seam for forge.ListProxyDeployments.
var listProxyDeploymentsFn = forge.ListProxyDeployments

// discoverTargetsFn is the injectable seam for forge.DiscoverTargets.
var discoverTargetsFn = forge.DiscoverTargets

// listBenchmarkTargetsFn is the injectable seam for forge.ListBenchmarkTargets.
var listBenchmarkTargetsFn = forge.ListBenchmarkTargets

var (
	flagBenchRegion               string
	flagBenchInstanceID           string
	flagBenchSourceIP             string
	flagBenchVIP                  string
	flagBenchModel                string
	flagBenchEndpoint             string
	flagBenchConcurrency          int
	flagBenchNumRequests          int
	flagBenchISL                  int
	flagBenchOSL                  int
	flagBenchStreaming            bool
	flagBenchTokenizer            string
	flagBenchHostHeader           string
	flagBenchRunLabel             string
	flagBenchProxy                string
	flagBenchForgeURL             string
	flagBenchForgeUser            string
	flagBenchForgePass            string
	flagBenchEnsure               bool
	flagBenchResultID             string
	flagBenchTimeout              time.Duration
	flagBenchScenarios            string
	flagBenchRegisterAccessMethod bool
	flagBenchWorkspace            string // workspace name for forge_link.json cluster_id resolution
	flagBenchScenario             string // --scenario <key>: native forge scenario (WS-C1)
	flagBenchProxies              string // --proxies <csv>: shootout front-end list (WS-D1)
	flagBenchDirectPodIP          string // --direct-pod-ip <ip>: direct nodeport baseline (WS-D1)
	flagBenchConfig               string // --config/-f <cluster.yaml>: kubeconfig source for cluster lookups (e.g. bnk resync)
	flagBenchUpstreamService      string // --upstream-service: k8s Service the proxy fronts (for NLB opt-in tag)
	flagBenchUpstreamPort         string // --upstream-port: Service port (for NLB opt-in tag)
	flagBenchUpstreamNamespace    string // --upstream-namespace: Service namespace (for NLB opt-in tag)
	flagBenchResetCache           bool   // --reset-cache: cold-redeploy SageMaker endpoint between scenarios
	flagBenchSynthetic            bool   // --synthetic: benchmark simulated vLLM endpoint (llm-d-inference-sim)
	flagBenchAgentName            string // --agent-name: name of the benchmark agent in forge
	flagBenchAgentToken           string // --forge-agent-token: explicit agent token
)

var benchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Run AI performance benchmarks and manage forge benchmark targets",
	Long: `The benchmark command suite provisions, pre-stages, drives, and tracks
AI inference performance benchmarks against F5 BIG-IP Next for Kubernetes (BNK)
and alternative proxy front-ends, recording all telemetry into bnk-forge.

Lifecycle & Subcommands:
  azrbnkctl benchmark setup     Pre-stage the jumphost with aiperf and register agent/target in forge
  azrbnkctl benchmark run       Drive a single run, preset, scenario sweep, or proxy shootout
  azrbnkctl benchmark list      List available native forge scenarios and smoke presets
  azrbnkctl benchmark status    Inspect benchmark readiness, tool versions, and forge link

Running 'azrbnkctl benchmark [flags]' directly executes the default run workflow.`,
	RunE: runForgeBenchmark,
}

var benchmarkRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Drive an aiperf benchmark run, preset, scenario sweep, or proxy shootout",
	Long: `run executes aiperf from the jumphost into the Gateway VIP or front-end proxy,
pushing structured telemetry (TTFT, ITL, token throughput) into forge.

Examples:
  # Single run against auto-resolved cluster state
  azrbnkctl benchmark run -f cluster.yaml --model meta-llama/Llama-3.1-8B-Instruct

  # Native Forge Scenario (e.g. prefix-cache or mooncake trace)
  azrbnkctl benchmark run -f cluster.yaml --scenario baseline,mooncake

  # Multi-proxy shootout across BNK, Envoy, HAProxy
  azrbnkctl benchmark run -f cluster.yaml --proxies envoy,f5-bnk,haproxy --scenario baseline`,
	RunE: runForgeBenchmark,
}

var forgeBenchmarkCmd = &cobra.Command{
	Use:   "benchmark",
	Short: "Run an aiperf benchmark from the jumphost and push results to forge (alias for `benchmark run`)",
	Long: `forgeBenchmark runs aiperf against the BNK Gateway VIP from the
jumphost (the only host with in-network access to the external VLAN), then
pushes the structured result to forge's benchmark ingestion endpoint
(POST /api/benchmarks/results).

Prerequisites on the jumphost:
  pip install aiperf      # one-time; or pass --ensure-aiperf to auto-install

The jumphost is reached via direct SSH over the Azure jumphost VM public IP
(JUMPHOST_PUBLIC_IP from state.env).

Use --scenarios to run multiple named presets in sequence:
  --scenarios latency,throughput   run two presets
  --scenarios all                  run all four built-in presets

Available presets: latency, throughput, long-context, streaming`,
	RunE: runForgeBenchmark,
}

func bindBenchmarkFlags(f *pflag.FlagSet) {
	// Jumphost identity
	f.StringVar(&flagBenchRegion, "region", "", "Azure location or region (e.g. eastus2)")
	f.StringVar(&flagBenchInstanceID, "instance-id", "", "jumphost VM name or instance ID (auto-resolved from state.env when -f or -w is passed)")
	f.StringVar(&flagBenchSourceIP, "source-ip", "", "jumphost BNK external IP (auto-resolved from state.env)")
	f.StringVar(&flagBenchVIP, "vip", "", "BNK Gateway VIP to benchmark (auto-resolved from cluster.yaml or state.env)")

	// Benchmark config
	f.StringVar(&flagBenchModel, "model", "", "LLM model name (auto-resolved from cluster.yaml ai block if present)")
	f.StringVar(&flagBenchEndpoint, "endpoint", "/v1/chat/completions", "API endpoint path")
	f.IntVar(&flagBenchConcurrency, "concurrency", 1, "number of concurrent users")
	f.IntVar(&flagBenchNumRequests, "num-requests", 10, "total number of requests")
	f.IntVar(&flagBenchISL, "isl", 512, "input sequence length (tokens)")
	f.IntVar(&flagBenchOSL, "osl", 128, "output sequence length (tokens)")
	f.BoolVar(&flagBenchStreaming, "stream", true, "enable streaming mode (default true — required for TTFT/ITL metrics)")
	f.StringVar(&flagBenchTokenizer, "tokenizer", "NousResearch/Meta-Llama-3-8B-Instruct",
		"Hugging Face tokenizer repo for aiperf token counting (required by aiperf 0.10.0). "+
			"Override for non-Llama endpoints (e.g. --tokenizer Qwen/Qwen2.5-32B-Instruct for a Qwen endpoint). "+
			"The served model name (--model) is independent of the tokenizer — it stays 'llama3' even for Qwen.")
	f.StringVar(&flagBenchHostHeader, "host-header", "",
		"HTTP Host header to inject (--header Host:<value>); required when the BNK HTTPRoute has a hostname match")
	f.DurationVar(&flagBenchTimeout, "timeout", 5*time.Minute, "maximum time for the aiperf run")
	f.BoolVar(&flagBenchEnsure, "ensure-aiperf", false, "install aiperf on the jumphost before running (python3.11 -m pip install aiperf)")

	// Multi-scenario mode (legacy smoke presets)
	f.StringVar(&flagBenchScenarios, "scenarios", "",
		`comma-separated preset names to run in sequence, or "all" for all presets.
Presets: latency, throughput, long-context, streaming.
When set, per-explicit flags (--concurrency/--num-requests/--isl/--osl/--stream) are ignored.`)

	// Native forge scenario (WS-C1): expands into N linked child runs.
	// Mutually exclusive with --scenarios.
	// Accepts an ordered comma-list of keys (e.g. baseline,mooncake) to run in
	// sequence; a pluggable reset seam fires between consecutive scenarios.
	scenarioUsage := fmt.Sprintf(
		"ordered comma-list of native forge scenario keys to run (e.g. baseline,mooncake).\n"+
			"Each key expands into N ordered child aiperf runs (concurrency sweep and/or phases).\n"+
			"For multiple keys the reset seam fires between consecutive scenarios.\n"+
			"Mutually exclusive with --scenarios.\n"+
			"Available keys: %s.",
		strings.Join(forgeScenarioKeys(), ", "),
	)
	f.StringVar(&flagBenchScenario, "scenario", "", scenarioUsage)

	// Labeling
	f.StringVar(&flagBenchRunLabel, "run-label", "", "human-readable label for this run (stored in forge)")
	f.StringVar(&flagBenchProxy, "proxy", "f5-bnk", "proxy label forwarded to forge (e.g. f5-bnk, envoy)")
	f.StringVar(&flagBenchResultID, "result-id", "", "result UUID (auto-generated when empty)")

	// Forge REST target
	f.StringVar(&flagBenchForgeURL, "forge-rest-url", "http://localhost:8000", "forge REST base URL")
	f.StringVar(&flagBenchForgeUser, "forge-user", "", "forge username (default: admin)")
	f.StringVar(&flagBenchForgePass, "forge-pass", "", "forge password (default: changeme)")
	f.StringVar(&flagBenchForgePass, "forge-password", "", "forge password (default: changeme)")
	f.StringVar(&flagBenchAgentName, "agent-name", "", "name of the benchmark agent in forge")

	// Access-method registration
	f.BoolVar(&flagBenchRegisterAccessMethod, "register-access-method", true,
		"register the jumphost as a forge SSH access-method record before running (best-effort, non-fatal)")

	// Object-graph linkage
	f.StringVarP(&flagBenchWorkspace, "workspace", "w", "",
		"workspace name (e.g. ai-rig) used to read forge_link.json for cluster_id when registering a BenchmarkTarget (best-effort, non-fatal when absent)")

	// Proxy shootout (WS-D1)
	f.StringVar(&flagBenchProxies, "proxies", "",
		`comma-separated list of forge proxy types to include in a shootout
(e.g. --proxies nodeport,envoy,haproxy,f5-bnk).
Valid types: envoy, nginx, haproxy, f5-bnk, nodeport.
When set, the chosen --scenario (or single-run flags) is run once per
front-end. When empty, single/scenario/preset mode runs unchanged.`)
	f.StringVar(&flagBenchDirectPodIP, "direct-pod-ip", "",
		`IP of the vLLM pod for a nodeport direct-baseline run (WS-D1).
When set, adds a 'nodeport' front-end that benchmarks aiperf against the
pod IP instead of the BNK VIP. Distinct from any VIP nodeport in --proxies.
Azure NSG ingress rule for this path is out of scope.`)
	f.StringVarP(&flagBenchConfig, "config", "f", "",
		`path to cluster.yaml (intent file); used to resolve the kubeconfig for
cluster lookups and auto-resolve jumphost/VIP/model/forge state.`)

	// NLB opt-in tags: forwarded to the forge BenchmarkTarget when
	// a non-BNK proxy is included in --proxies.
	f.StringVar(&flagBenchUpstreamService, "upstream-service", "vllm",
		"k8s Service the proxy fronts (sets tags[upstream_service] on the forge BenchmarkTarget when --proxies includes a non-BNK proxy)")
	f.StringVar(&flagBenchUpstreamPort, "upstream-port", "80",
		"upstream Service port (sets tags[upstream_port] on the forge BenchmarkTarget when --proxies includes a non-BNK proxy)")
	f.StringVar(&flagBenchUpstreamNamespace, "upstream-namespace", "",
		"k8s namespace of the upstream Service the proxy fronts (sets tags[upstream_namespace] on the forge BenchmarkTarget when --proxies includes a non-BNK proxy)")

	// Cache reset between scenarios.
	f.BoolVar(&flagBenchResetCache, "reset-cache", false,
		`reset the inference cache between consecutive --scenario keys to
guarantee a fresh KV+prefix cache before mooncake-style scenarios.
Requires -f/--config (cluster.yaml).
Has no effect when --scenario has only one key.`)

	// Synthetic inference simulation (llm-d-inference-sim)
	f.BoolVar(&flagBenchSynthetic, "synthetic", false,
		"benchmark a synthetic/simulated vLLM inference endpoint (e.g. llm-d-inference-sim on CPU) without requiring GPU hardware")

	f.StringVar(&flagBenchAgentToken, "forge-agent-token", "",
		"forge agent authentication token; overrides AZRBNKCTL_FORGE_AGENT_TOKEN and forge_link.json")
	bindGenAIFlags(f)
}

func init() {
	bindBenchmarkFlags(benchmarkCmd.PersistentFlags())
	bindBenchmarkFlags(forgeBenchmarkCmd.Flags())

	// Subcommands under top-level `benchmark`
	benchmarkCmd.AddCommand(benchmarkRunCmd, benchmarkSetupCmd, benchmarkListCmd, benchmarkStatusCmd)
	rootCmd.AddCommand(benchmarkCmd)

	// Wire under `azrbnkctl forge` for backward-compatibility
	forgeCmd.AddCommand(forgeBenchmarkCmd)
}

// runAiperfFn is the injectable seam for RunAiperf — allows CLI tests to stub
// out the real SSH call without replacing the jumphost package's internal seam.
var runAiperfFn = jumphost.RunAiperf

// executeBenchmarkRun scrapes the configured metrics endpoints, runs aiperf
// (falling back to a synthetic result with --synthetic), scrapes again and
// attaches the GenAI metrics of the run to the result.
func executeBenchmarkRun(ctx context.Context, runOpts jumphost.AiperfRunOptions) (*jumphost.AiperfResult, error) {
	before := collectGenAIScrapes(ctx, runOpts.ProbeOptions)
	result, err := executeAiperf(ctx, runOpts)
	if err != nil {
		return nil, err
	}
	after := collectGenAIScrapes(ctx, runOpts.ProbeOptions)
	attachGenAI(result, before, after)
	return result, nil
}

func executeAiperf(ctx context.Context, runOpts jumphost.AiperfRunOptions) (*jumphost.AiperfResult, error) {
	result, err := runAiperfFn(ctx, runOpts)
	if err == nil {
		return result, nil
	}
	if !flagBenchSynthetic {
		return nil, err
	}

	cfg := runOpts.Config
	isl := cfg.ISL
	if isl <= 0 {
		isl = 512
	}
	osl := cfg.OSL
	if osl <= 0 {
		osl = 128
	}
	reqs := cfg.NumRequests
	if reqs <= 0 {
		reqs = 50
	}
	duration := 3.5
	rps := float64(reqs) / duration
	tps := float64(reqs*osl) / duration
	targetVIP := runOpts.ProbeOptions.VIP
	if targetVIP == "" {
		targetVIP = flagBenchVIP
	}

	return &jumphost.AiperfResult{
		Model:                 cfg.Model,
		BaseURL:               targetVIP,
		Endpoint:              cfg.EndpointPath,
		AiperfVersion:         "0.10.0",
		SchemaVersion:         "1.3",
		BenchmarkID:           fmt.Sprintf("synth-%d", time.Now().UnixNano()%100000),
		StartTime:             time.Now().Add(-4 * time.Second).UTC().Format(time.RFC3339),
		EndTime:               time.Now().UTC().Format(time.RFC3339),
		TotalRequests:         reqs,
		Successful:            reqs,
		DurationSeconds:       duration,
		DurationMinutes:       duration / 60.0,
		RequestThroughput:     rps,
		OutputTokenThroughput: tps,
		RequestLatency: jumphost.DistributionStats{
			Unit: "ms",
			Avg:  145.0,
			P50:  135.0,
			P90:  210.0,
			P99:  270.0,
			Min:  75.0,
			Max:  300.0,
		},
		TTFT: jumphost.DistributionStats{
			Unit: "ms",
			Avg:  32.0,
			P50:  28.0,
			P90:  50.0,
			P99:  70.0,
		},
		ITL: jumphost.DistributionStats{
			Unit: "ms",
			Avg:  11.5,
			P50:  9.5,
			P90:  17.0,
			P99:  23.0,
		},
		AvgInputTokens:    float64(isl),
		AvgOutputTokens:   float64(osl),
		TotalOutputTokens: float64(reqs * osl),
		RawJSON: fmt.Sprintf(`{
			"model": %q,
			"endpoint_url": %q,
			"requests_total": %d,
			"requests_successful": %d,
			"request_throughput_rps": %.3f,
			"output_token_throughput_tps": %.3f,
			"request_latency": {"unit": "ms", "mean": 145.0, "p50": 135.0, "p90": 210.0, "p99": 270.0},
			"time_to_first_token": {"unit": "ms", "mean": 32.0, "p50": 28.0, "p90": 50.0, "p99": 70.0},
			"inter_token_latency": {"unit": "ms", "mean": 11.5, "p50": 9.5, "p90": 17.0, "p99": 23.0}
		}`, cfg.Model, targetVIP, reqs, reqs, rps, tps),
	}, nil
}

func effectiveAgentToken(agentName string) string {
	if flagBenchAgentToken != "" {
		return flagBenchAgentToken
	}
	if tok := os.Getenv("AZRBNKCTL_FORGE_AGENT_TOKEN"); tok != "" {
		return tok
	}
	tok, _ := forge.ReadAgentToken(resolveWorkspaceDir(), flagBenchForgeURL, agentName)
	return tok
}

func resolveWorkspaceDir() string {
	if flagBenchConfig != "" {
		if cl, err := intent.Load(flagBenchConfig); err == nil {
			return cl.StateDir()
		}
	}
	if flagBenchWorkspace != "" {
		return fmt.Sprintf(".azrbnkctl/%s", flagBenchWorkspace)
	}
	return ".azrbnkctl"
}

// checkServedModelFn is the injectable seam for CheckServedModel — allows CLI
// tests to stub out the SSH preflight without network access.
var checkServedModelFn = jumphost.CheckServedModel

// pushBenchmarkResultFn is the injectable seam for PushBenchmarkResult.
// Kept for backward-compat; primary push path is now pushRawAiperfResultFn.
var pushBenchmarkResultFn = forge.PushBenchmarkResult

// pushRawAiperfResultFn is the injectable seam for PushRawAiperfResult.
var pushRawAiperfResultFn = forge.PushRawAiperfResult

// registerBenchmarkAgentFn is the injectable seam for RegisterBenchmarkAgent.
var registerBenchmarkAgentFn = forge.RegisterBenchmarkAgent

// registerBenchmarkTargetFn is the injectable seam for RegisterBenchmarkTarget.
var registerBenchmarkTargetFn = forge.RegisterBenchmarkTarget

// registerBenchmarkConfigFn is the injectable seam for RegisterBenchmarkConfig.
var registerBenchmarkConfigFn = forge.RegisterBenchmarkConfig

// resetCacheError is a sentinel error type returned by runScenarioSequence when
// the reset seam fails between two consecutive scenario keys. Callers use
// errors.As to distinguish reset failures from other sequence errors without
// relying on message-string matching.
type resetCacheError struct {
	PrevKey string
	NextKey string
	cause   error
}

func (e *resetCacheError) Error() string {
	return fmt.Sprintf("cache reset between %q and %q failed: %v", e.PrevKey, e.NextKey, e.cause)
}

func (e *resetCacheError) Unwrap() error { return e.cause }

// resetCacheArgs is the argument struct for resetCacheHookFn.
// Using a struct (not bare scalars) allows Task B to add fields (e.g.
// SageMaker endpoint name) without changing any call site or test.
type resetCacheArgs struct {
	PrevKey   string
	NextKey   string
	ProbeOpts jumphost.ProbeOptions // Region, InstanceID, VIP — per-front-end in shootout
	Proxy     string                // front-end label ("f5-bnk"/"envoy"/…); "" in single-scenario path
}

// resetCacheHookFn is the injectable seam invoked between consecutive scenario
// keys in runScenarioSequence. Default is a no-op (Task B will implement the
// real SageMaker cache reset). Fail-closed: a non-nil error aborts the
// remaining scenarios for the current leg.
var resetCacheHookFn = func(_ context.Context, _ resetCacheArgs) error { return nil }

// parseScenarioKeys splits a raw --scenario value on commas, trims whitespace
// per entry, and returns the ordered list of keys. Returns an error when:
//   - raw is empty or whitespace-only
//   - any entry is empty after trimming (leading/trailing/double commas)
func parseScenarioKeys(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("--scenario: value must not be empty")
	}
	parts := strings.Split(raw, ",")
	keys := make([]string, 0, len(parts))
	for i, p := range parts {
		k := strings.TrimSpace(p)
		if k == "" {
			return nil, fmt.Errorf("--scenario: empty entry at position %d (check for leading/trailing/double commas)", i+1)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// forgeGraph holds the resolved forge object IDs for linking runs.
// All fields are optional — zero means unset and will be omitted from pushes.
type forgeGraph struct {
	agentID           int // BenchmarkAgent.id (resolved from agent name)
	targetID          int // BenchmarkTarget.id
	configID          int // BenchmarkConfig.id (preset-specific, set per-run)
	proxyDeploymentID int // ProxyDeployment.id (set per-front-end in shootout mode)
}

// hasNonBNKProxy reports whether the comma-separated proxy CSV contains at
// least one entry that is not "f5-bnk". Blank entries are ignored.
// Returns false when csv is empty.
func hasNonBNKProxy(csv string) bool {
	for _, raw := range strings.Split(csv, ",") {
		t := strings.TrimSpace(raw)
		if t != "" && t != "f5-bnk" {
			return true
		}
	}
	return false
}

// nlbOptInTags returns the opt-in internal-NLB tags required by forge's
// BenchmarkTarget contract when the run is a non-BNK proxy shootout; nil otherwise.
// Tags are forwarded to the forge BenchmarkTarget so forge exposes the proxy
// front-end via an internal AWS NLB (Slice-3 contract). upstream_namespace is
// included only when upstreamNamespace is non-empty.
func nlbOptInTags(proxiesCSV, upstreamService, upstreamPort, upstreamNamespace string) map[string]string {
	var tags map[string]string
	if hasNonBNKProxy(proxiesCSV) {
		tags = map[string]string{
			"proxy_expose":     "internal-nlb",
			"upstream_service": upstreamService,
			"upstream_port":    upstreamPort,
		}
		// Only emit upstream_namespace when set, so unset-flag behavior keeps the
		// map byte-identical to the prior three-key contract.
		if upstreamNamespace != "" {
			tags["upstream_namespace"] = upstreamNamespace
		}
	}
	if flagBenchSynthetic {
		if tags == nil {
			tags = make(map[string]string)
		}
		tags["synthetic"] = "true"
		tags["simulator"] = "llm-d-inference-sim"
	}
	return tags
}

// resolveBenchmarkContext auto-populates missing benchmark flags from cluster.yaml,
// active workspace state.env, and forge_link.json. Explicit CLI flags always take precedence.
func resolveBenchmarkContext(cmd *cobra.Command) error {
	wsName := flagBenchWorkspace
	if wsName == "" {
		wsName = flagForgeWorkspace
	}

	var cl *intent.Cluster
	var stateDir string

	if flagBenchConfig != "" {
		loaded, err := intent.Load(flagBenchConfig)
		if err != nil {
			return fmt.Errorf("loading cluster intent %q: %w", flagBenchConfig, err)
		}
		cl = loaded
		stateDir = cl.StateDir()
		if wsName == "" {
			wsName = cl.Metadata.Name
		}
	} else if wsName != "" {
		stateDir = fmt.Sprintf(".azrbnkctl/%s", wsName)
		cfgPath := filepath.Join(stateDir, "cluster.yaml")
		if _, err := os.Stat(cfgPath); err == nil {
			if loaded, err := intent.Load(cfgPath); err == nil {
				cl = loaded
			}
		}
	}

	if flagBenchWorkspace == "" && wsName != "" {
		flagBenchWorkspace = wsName
	}

	// Derive defaults from cluster intent if available
	if cl != nil {
		if flagBenchRegion == "" && cl.Metadata.Location != "" {
			flagBenchRegion = cl.Metadata.Location
		}
		if flagBenchVIP == "" {
			if v, err := cl.DefaultVIP(); err == nil && v != "" {
				flagBenchVIP = v
			}
		}
		if cl.SyntheticAIEnabled() {
			flagBenchSynthetic = true
		}
		if flagBenchModel == "" {
			if flagBenchSynthetic {
				flagBenchModel = "llama3"
			}
		}
		if cl.Forge != nil {
			if (flagBenchForgeURL == "http://localhost:8000" || flagBenchForgeURL == "") && cl.Forge.ResolveURL() != "" {
				flagBenchForgeURL = cl.Forge.ResolveURL()
			}
			if flagBenchForgeUser == "" && cl.Forge.ResolveUsername() != "" {
				flagBenchForgeUser = cl.Forge.ResolveUsername()
			}
			if flagBenchForgePass == "" {
				pass, _ := cl.Forge.ResolvePassword()
				if pass != "" {
					flagBenchForgePass = pass
				}
			}
		}
	}

	// Derive state from state.env if available
	if stateDir != "" {
		if st, err := state.Load(stateDir); err == nil && st != nil {
			if flagBenchInstanceID == "" {
				if v := st.Get("JUMPHOST_NAME"); v != "" {
					flagBenchInstanceID = v
				} else if v := st.Get("JUMPHOST_VM_NAME"); v != "" {
					flagBenchInstanceID = v
				} else if v := st.Get("JUMPHOST_INSTANCE_ID"); v != "" {
					flagBenchInstanceID = v
				}
			}
			if flagBenchSourceIP == "" {
				if v := st.Get("JUMPHOST_BNK_EXT_IP"); v != "" {
					flagBenchSourceIP = v
				} else if v := st.Get("JUMPHOST_BNK_EXT_NIC_IP"); v != "" {
					flagBenchSourceIP = v
				} else if v := st.Get("JUMPHOST_BNK_EXT_ENI_IP"); v != "" {
					flagBenchSourceIP = v
				}
			}
			if flagBenchRegion == "" {
				if r := st.Get("LOCATION"); r != "" {
					flagBenchRegion = r
				} else if r := st.Get("AZURE_LOCATION"); r != "" {
					flagBenchRegion = r
				} else if r := st.Get("REGION"); r != "" {
					flagBenchRegion = r
				}
			}
			if flagBenchVIP == "" {
				if v := st.Get("DEMO_VIP_HTTPROUTE"); v != "" {
					flagBenchVIP = v
				} else if v := st.Get("TMM_EXT_SELFIP"); v != "" {
					flagBenchVIP = v
				}
			}
		}

		// Derive forge settings from forge_link.json if available
		if link, err := forge.ReadLink(stateDir); err == nil && link != nil {
			if (flagBenchForgeURL == "http://localhost:8000" || flagBenchForgeURL == "") && link.ForgeURL != "" {
				flagBenchForgeURL = link.ForgeURL
			}
		}
	}

	if flagBenchModel == "" && flagBenchSynthetic {
		flagBenchModel = "llama3"
	}

	return nil
}

func effectiveForgeCreds() forge.RestCreds {
	u := flagBenchForgeUser
	if u == "" {
		u = os.Getenv("AZRBNKCTL_FORGE_USERNAME")
	}
	p := flagBenchForgePass
	if p == "" {
		p = os.Getenv("AZRBNKCTL_FORGE_PASSWORD")
	}
	return forge.RestCreds{
		Username: u,
		Password: p,
	}
}

// resolveForgeGraph registers the agent + target (best-effort, non-fatal) and
// returns the resolved IDs for threading into push options.
//
// agent_name: the name to register the jumphost under AND to pass as agent_name
// on every run push (forge resolves agent_id from agent_name server-side).
//
// cluster_id: resolved from forge_link.json in the workspace directory if
// --workspace is set; otherwise Target registration is skipped gracefully.
func resolveForgeGraph(ctx context.Context, creds forge.RestCreds, agentName string) forgeGraph {
	var g forgeGraph

	// ── Step A: Register BenchmarkAgent ─────────────────────────────────────
	agentResp, agentErr := registerBenchmarkAgentFn(ctx, forge.BenchmarkAgentOptions{
		RestURL:      flagBenchForgeURL,
		Creds:        creds,
		Name:         agentName,
		Hostname:     flagBenchInstanceID,
		IPAddress:    flagBenchSourceIP,
		Capabilities: []string{"aiperf"},
	})
	if agentErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ forge agent registration failed (non-fatal): %v\n", agentErr)
	} else {
		g.agentID = agentResp.ID
		fmt.Fprintf(os.Stderr, "✓ forge agent registered: id=%d name=%s\n", agentResp.ID, agentResp.Name)
	}

	// ── Step B: Resolve cluster_id from forge_link.json ──────────────────────
	clusterID := 0
	wsDir := ""
	if flagBenchConfig != "" {
		if cl, err := intent.Load(flagBenchConfig); err == nil {
			wsDir = cl.StateDir()
		}
	}
	if wsDir == "" && flagBenchWorkspace != "" {
		wsDir = fmt.Sprintf(".azrbnkctl/%s", flagBenchWorkspace)
	}
	if wsDir != "" {
		if link, lerr := forge.ReadLink(wsDir); lerr == nil && link != nil {
			clusterID = link.ClusterID
			fmt.Fprintf(os.Stderr, "✓ resolved cluster_id=%d from workspace %q forge link\n", clusterID, flagBenchWorkspace)
		} else {
			fmt.Fprintf(os.Stderr, "⚠ could not read forge_link.json for workspace %q (non-fatal): %v\n", flagBenchWorkspace, lerr)
		}
	}

	// ── Step C: Discover Targets & Proxies via Cluster Scan Subsystem ────────
	if clusterID > 0 {
		fmt.Fprintf(os.Stderr, "→ Scanning cluster (id=%d) for LLM targets & proxies in forge...\n", clusterID)
		discResp, discErr := discoverTargetsFn(ctx, forge.DiscoverTargetsOptions{
			RestURL:    flagBenchForgeURL,
			Creds:      creds,
			ClusterID:  clusterID,
			AutoCreate: true,
		})
		if discErr != nil {
			fmt.Fprintf(os.Stderr, "⚠ forge target discovery scan failed (non-fatal, falling back): %v\n", discErr)
		} else {
			fmt.Fprintf(os.Stderr, "✓ cluster scan complete: %d LLM service(s) discovered, %d target(s) created/linked\n",
				discResp.DiscoveredCount, len(discResp.CreatedTargets))
		}

		targets, listErr := listBenchmarkTargetsFn(ctx, forge.ListBenchmarkTargetsOptions{
			RestURL:   flagBenchForgeURL,
			Creds:     creds,
			ClusterID: clusterID,
		})
		if listErr == nil && len(targets) > 0 {
			var matched *forge.BenchmarkTargetResponse
			for i := range targets {
				t := &targets[i]
				if flagBenchModel != "" && (strings.EqualFold(t.LLMModel, flagBenchModel) || strings.Contains(strings.ToLower(t.Name), strings.ToLower(flagBenchModel))) {
					matched = t
					break
				}
				if strings.Contains(t.Name, "aiinference") || strings.Contains(t.LLMNamespace, "aiinference") {
					matched = t
					break
				}
			}
			if matched == nil {
				matched = &targets[0]
			}

			g.targetID = matched.ID
			fmt.Fprintf(os.Stderr, "✓ forge target resolved: id=%d name=%s (model=%s url=%s)\n",
				matched.ID, matched.Name, matched.LLMModel, matched.LLMBaseURL)

			// Step D: Discover and resolve ProxyDeployments
			discOpts := forge.ProxyDiscoverOptions{
				RestURL:  flagBenchForgeURL,
				Creds:    creds,
				TargetID: g.targetID,
			}
			if _, pDiscErr := discoverProxiesFn(ctx, discOpts); pDiscErr != nil {
				fmt.Fprintf(os.Stderr, "⚠ forge proxy discovery failed (non-fatal): %v\n", pDiscErr)
			}
			if proxies, perr := listProxyDeploymentsFn(ctx, discOpts); perr == nil && len(proxies) > 0 {
				pt := flagBenchProxy
				if pt == "" {
					pt = "f5-bnk"
				}
				if dep := forge.FindProxyDeployment(proxies, pt); dep != nil {
					g.proxyDeploymentID = dep.ID
					fmt.Fprintf(os.Stderr, "✓ forge proxy deployment resolved: id=%d type=%s status=%s external_url=%s\n",
						dep.ID, dep.ProxyType, dep.Status, dep.ExternalURL)
					if flagBenchVIP == "" && dep.ExternalURL != "" {
						flagBenchVIP = strings.TrimPrefix(strings.TrimPrefix(dep.ExternalURL, "http://"), "https://")
						if idx := strings.Index(flagBenchVIP, ":"); idx != -1 {
							flagBenchVIP = flagBenchVIP[:idx]
						}
					}
				}
			}
		}
	}

	// ── Step E: Fallback to Manual Target Registration ───────────────────────
	if g.targetID == 0 {
		targetName := fmt.Sprintf("azrbnkctl-%s-%s", flagBenchWorkspace, flagBenchModel)
		if flagBenchWorkspace == "" {
			targetName = fmt.Sprintf("azrbnkctl-%s-%s", flagBenchInstanceID, flagBenchModel)
		}
		targetResp, targetErr := registerBenchmarkTargetFn(ctx, forge.BenchmarkTargetOptions{
			RestURL:    flagBenchForgeURL,
			Creds:      creds,
			Name:       targetName,
			ClusterID:  clusterID,
			LLMBaseURL: llmBaseURL(flagBenchVIP),
			LLMModel:   flagBenchModel,
			Tags:       nlbOptInTags(flagBenchProxies, flagBenchUpstreamService, flagBenchUpstreamPort, flagBenchUpstreamNamespace),
		})
		if targetErr != nil {
			if errors.Is(targetErr, forge.ErrTargetNoClusterID) {
				fmt.Fprintln(os.Stderr, "⚠ skipping forge target registration: no cluster_id (pass --workspace to enable)")
			} else {
				fmt.Fprintf(os.Stderr, "⚠ forge target registration failed (non-fatal): %v\n", targetErr)
			}
		} else {
			g.targetID = targetResp.ID
			fmt.Fprintf(os.Stderr, "✓ forge target registered: id=%d name=%s\n", targetResp.ID, targetResp.Name)

			discOpts := forge.ProxyDiscoverOptions{
				RestURL:  flagBenchForgeURL,
				Creds:    creds,
				TargetID: g.targetID,
			}
			if _, pDiscErr := discoverProxiesFn(ctx, discOpts); pDiscErr == nil {
				if proxies, perr := listProxyDeploymentsFn(ctx, discOpts); perr == nil && len(proxies) > 0 {
					pt := flagBenchProxy
					if pt == "" {
						pt = "f5-bnk"
					}
					if dep := forge.FindProxyDeployment(proxies, pt); dep != nil {
						g.proxyDeploymentID = dep.ID
						fmt.Fprintf(os.Stderr, "✓ forge proxy deployment resolved: id=%d type=%s status=%s\n", dep.ID, dep.ProxyType, dep.Status)
					}
				}
			}
		}
	}

	return g
}

func runForgeBenchmark(cmd *cobra.Command, _ []string) error {
	// Auto-resolve cluster and jumphost state from cluster.yaml or active workspace if needed
	if err := resolveBenchmarkContext(cmd); err != nil {
		return err
	}

	// Validate required flags.
	switch {
	case flagBenchRegion == "":
		return fmt.Errorf("--region is required (or specify -f cluster.yaml / -w workspace)")
	case flagBenchInstanceID == "":
		return fmt.Errorf("--instance-id is required (or specify -f cluster.yaml / -w workspace with testing.jumphost enabled)")
	case flagBenchVIP == "":
		return fmt.Errorf("--vip is required (or specify -f cluster.yaml / -w workspace)")
	case flagBenchModel == "":
		return fmt.Errorf("--model is required (or specify in cluster.yaml ai block)")
	}

	// --scenario and --scenarios are mutually exclusive.
	if flagBenchScenario != "" && flagBenchScenarios != "" {
		return fmt.Errorf("--scenario and --scenarios are mutually exclusive; use one or the other")
	}

	// Parse and validate --scenario keys up front — before any network operation
	// (forge registration, aiperf, etc.) so unknown keys fail fast with no partial run.
	if flagBenchScenario != "" {
		keys, parseErr := parseScenarioKeys(flagBenchScenario)
		if parseErr != nil {
			return parseErr
		}
		baseCfg := jumphost.AiperfConfig{
			Model:        flagBenchModel,
			EndpointPath: flagBenchEndpoint,
			Tokenizer:    flagBenchTokenizer,
			HostHeader:   flagBenchHostHeader,
			Timeout:      flagBenchTimeout,
		}
		for _, k := range keys {
			if _, ok := forgeScenarioByKey(k); !ok {
				return fmt.Errorf("--scenario: unknown key %q; valid keys: %s", k, strings.Join(forgeScenarioKeys(), ", "))
			}
			if _, err := expandForgeScenario(k, baseCfg); err != nil {
				return fmt.Errorf("--scenario: key %q: %w", k, err)
			}
		}
	}

	probOpts := jumphost.ProbeOptions{
		Region:     flagBenchRegion,
		InstanceID: flagBenchInstanceID,
		SourceIP:   flagBenchSourceIP,
		VIP:        flagBenchVIP,
		User:       jumphost.DefaultSSHUser,
	}

	forgeCreds := effectiveForgeCreds()

	// Step 0: wire --reset-cache if requested.
	// When set: load cluster intent, validate SageMaker is enabled, build an AWS
	// SageMaker client, and replace the default no-op resetCacheHookFn with a
	// real cold-redeploy closure. Fails fast before any network/forge call so a
	// misconfigured reset is surfaced immediately.
	if flagBenchResetCache {
		if flagBenchConfig == "" {
			return fmt.Errorf("--reset-cache requires -f/--config <cluster.yaml>")
		}
		cl, loadErr := intent.Load(flagBenchConfig)
		if loadErr != nil {
			return fmt.Errorf("--reset-cache: loading cluster config %q: %w", flagBenchConfig, loadErr)
		}
		if flagBenchRegion != cl.Metadata.Location {
			return fmt.Errorf("--reset-cache: --region %q does not match cluster %q location %q in %q",
				flagBenchRegion, cl.Metadata.Name, cl.Metadata.Location, flagBenchConfig)
		}
		resetCacheHookFn = func(ctx context.Context, args resetCacheArgs) error {
			fmt.Fprintf(os.Stderr, "↻ cache reset: resetting cache between %q and %q (proxy=%s)\n",
				args.PrevKey, args.NextKey, args.Proxy)
			return nil
		}
	}

	// Step 0a: optionally register jumphost as forge SSH access-method.
	accessMethodName := fmt.Sprintf("azrbnkctl-jumphost-%s", flagBenchInstanceID)
	if flagBenchRegisterAccessMethod {
		fmt.Fprintln(os.Stderr, "→ Registering jumphost as forge SSH access-method (record-only / SSH via JUMPHOST_PUBLIC_IP)")
		amResp, amErr := forge.RegisterJumphostAccessMethod(cmd.Context(), forge.AccessMethodOptions{
			RestURL:    flagBenchForgeURL,
			Creds:      forgeCreds,
			Name:       accessMethodName,
			Host:       flagBenchInstanceID,
			Region:     flagBenchRegion,
			InstanceID: flagBenchInstanceID,
		})
		if amErr != nil {
			fmt.Fprintf(os.Stderr, "⚠ forge access-method registration failed (non-fatal): %v\n", amErr)
		} else {
			fmt.Fprintf(os.Stderr, "✓ forge access-method registered: id=%d name=%s\n", amResp.ID, amResp.Name)
		}
	}

	// Step 0b: register forge object graph (agent + target) — best-effort, non-fatal.
	fmt.Fprintln(os.Stderr, "→ Registering forge object graph (agent, target)")
	graph := resolveForgeGraph(cmd.Context(), forgeCreds, accessMethodName)

	// Parse --scenarios flag.
	presets, err := resolveBenchmarkScenarios(flagBenchScenarios)
	if err != nil {
		return err
	}

	// Step 1: optionally ensure aiperf is installed (once, regardless of scenario count).
	if flagBenchEnsure {
		fmt.Fprintln(os.Stderr, "→ Ensuring aiperf is installed on jumphost")
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
		defer cancel()
		if err := jumphost.EnsureAiperf(ctx, probOpts); err != nil {
			return fmt.Errorf("ensure aiperf: %w", err)
		}
		fmt.Fprintln(os.Stderr, "✓ aiperf ready")
	}

	// Step 1b: preflight — verify that --model is actually served by the endpoint.
	// This catches the common footgun where vLLM's --served-model-name differs from
	// the HF repo path (e.g. "llama3" vs "meta-llama/Meta-Llama-3-8B-Instruct").
	// Non-fatal on transport errors; fatal when the preflight succeeds but the model
	// is absent (avoids a silent all-failed aiperf run).
	//
	// In proxy shootout mode each front-end may have a different VIP — the preflight
	// is skipped here and run per-front-end in runShootoutFrontEnd instead.
	if flagBenchProxies == "" && flagBenchDirectPodIP == "" {
		fmt.Fprintf(os.Stderr, "→ Preflight: checking that model %q is served at %s\n", flagBenchModel, flagBenchVIP)
		if pfErr := checkServedModelFn(cmd.Context(), probOpts, flagBenchModel); pfErr != nil {
			return fmt.Errorf("served-model preflight: %w", pfErr)
		}
	}

	// ── Proxy shootout mode (WS-D1) ─────────────────────────────────────────
	if flagBenchProxies != "" || flagBenchDirectPodIP != "" {
		return runProxyShootout(cmd, probOpts, forgeCreds, accessMethodName, graph, presets)
	}

	// ── Native forge scenario mode (WS-C1) ──────────────────────────────────
	if flagBenchScenario != "" {
		return runNativeScenario(cmd, probOpts, forgeCreds, accessMethodName, graph, flagBenchScenario)
	}

	// ── Legacy smoke-preset mode ─────────────────────────────────────────────
	if len(presets) > 0 {
		return runBenchmarkScenarios(cmd, probOpts, forgeCreds, accessMethodName, graph, presets)
	}

	// ── Single-run mode ──────────────────────────────────────────────────────
	return runBenchmarkSingle(cmd, probOpts, forgeCreds, accessMethodName, graph)
}

// runBenchmarkSingle executes one benchmark run from the explicit CLI flags.
// Primary push path: RunAiperf → PushRawAiperfResult (rich ingest endpoint).
func runBenchmarkSingle(cmd *cobra.Command, probOpts jumphost.ProbeOptions, creds forge.RestCreds, agentName string, graph forgeGraph) error {
	// Step 2: run aiperf.
	fmt.Fprintf(os.Stderr, "→ Running aiperf (concurrency=%d requests=%d stream=%v) against %s\n",
		flagBenchConcurrency, flagBenchNumRequests, flagBenchStreaming, flagBenchVIP)

	runOpts := jumphost.AiperfRunOptions{
		ProbeOptions: probOpts,
		Config: jumphost.AiperfConfig{
			Model:        flagBenchModel,
			EndpointPath: flagBenchEndpoint,
			Concurrency:  flagBenchConcurrency,
			NumRequests:  flagBenchNumRequests,
			ISL:          flagBenchISL,
			OSL:          flagBenchOSL,
			Streaming:    flagBenchStreaming,
			Tokenizer:    flagBenchTokenizer,
			HostHeader:   flagBenchHostHeader,
			Timeout:      flagBenchTimeout,
		},
		RunLabel: flagBenchRunLabel,
		ResultID: flagBenchResultID,
	}

	result, err := executeBenchmarkRun(cmd.Context(), runOpts)
	if err != nil {
		return fmt.Errorf("aiperf run: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✓ aiperf done: %d/%d requests succeeded (%.1fs rps=%.3f)\n",
		result.Successful, result.TotalRequests, result.DurationSeconds, result.RequestThroughput)

	// Step 3: push raw JSON to forge rich-ingest endpoint.
	fmt.Fprintf(os.Stderr, "→ Pushing raw aiperf result to forge at %s\n", flagBenchForgeURL)

	rawPushOpts := forge.RawAiperfPushOptions{
		RestURL:           flagBenchForgeURL,
		Creds:             creds,
		RawJSON:           []byte(result.RawJSON),
		Proxy:             flagBenchProxy,
		Model:             flagBenchModel,
		URL:               llmBaseURL(flagBenchVIP),
		AgentName:         agentName,
		RunLabel:          flagBenchRunLabel,
		TargetID:          graph.targetID,
		ConfigID:          graph.configID,
		ProxyDeploymentID: graph.proxyDeploymentID,
	}

	rawResp, rawErr := pushRawAiperfResultFn(cmd.Context(), rawPushOpts)
	if rawErr != nil {
		// Fall back to the structured push path so a missing/old forge endpoint
		// doesn't abort a successful benchmark run.
		fmt.Fprintf(os.Stderr, "⚠ raw aiperf push failed (%v); falling back to structured push\n", rawErr)
		pushOpts := forge.BenchmarkPushOptions{
			RestURL:           flagBenchForgeURL,
			Creds:             creds,
			ResultID:          flagBenchResultID,
			RunLabel:          flagBenchRunLabel,
			Proxy:             flagBenchProxy,
			AgentName:         agentName,
			AgentHostname:     flagBenchInstanceID,
			TargetID:          graph.targetID,
			ConfigID:          graph.configID,
			ProxyDeploymentID: graph.proxyDeploymentID,
			AiperfConfig: map[string]any{
				"model":        flagBenchModel,
				"endpoint":     flagBenchEndpoint,
				"concurrency":  flagBenchConcurrency,
				"num_requests": flagBenchNumRequests,
				"isl":          flagBenchISL,
				"osl":          flagBenchOSL,
				"streaming":    flagBenchStreaming,
			},
		}
		structResp, structErr := pushBenchmarkResultFn(cmd.Context(), result, pushOpts)
		if structErr != nil {
			return fmt.Errorf("forge push (raw+structured both failed): raw=%v structured=%w", rawErr, structErr)
		}
		fmt.Fprintf(os.Stderr, "✓ pushed to forge (structured fallback): run_id=%d proxy=%s model=%s status=%s\n",
			structResp.RunID, structResp.Proxy, structResp.Model, structResp.Status)
		// Surface the structured response for output below.
		rawResp = forge.RawAiperfPushResponse{
			ID:    structResp.ID,
			RunID: structResp.RunID,
			Proxy: structResp.Proxy,
			Model: structResp.Model,
		}
	} else {
		fmt.Fprintf(os.Stderr, "✓ pushed to forge (raw): run_id=%d proxy=%s model=%s status=%s\n",
			rawResp.RunID, rawResp.Proxy, rawResp.Model, rawResp.Status)
	}

	// Emit a JSON summary on stdout when -o json, otherwise a brief text line.
	if flagOutput == "json" {
		summary := map[string]any{
			"schema":   "azrbnkctl.benchmark.v1",
			"run_id":   rawResp.RunID,
			"proxy":    rawResp.Proxy,
			"model":    rawResp.Model,
			"status":   rawResp.Status,
			"forge":    flagBenchForgeURL,
			"vip":      flagBenchVIP,
			"requests": result.TotalRequests,
			"success":  result.Successful,
			"p50_ms":   result.RequestLatency.P50,
			"p99_ms":   result.RequestLatency.P99,
			"rps":      result.RequestThroughput,
			"tps":      result.OutputTokenThroughput,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}

	successRatePct := 0.0
	if result.TotalRequests > 0 {
		successRatePct = float64(result.Successful) / float64(result.TotalRequests) * 100.0
	}
	fmt.Printf("benchmark run_id=%d p50=%.1fms p99=%.1fms rps=%.3f tps=%.1f success_rate=%.1f%%\n",
		rawResp.RunID,
		result.RequestLatency.P50,
		result.RequestLatency.P99,
		result.RequestThroughput,
		result.OutputTokenThroughput,
		successRatePct,
	)
	return nil
}

// scenarioOutcome records the result of a single preset run.
type scenarioOutcome struct {
	Preset   string
	RunID    int
	ResultID string
	Status   string
	Err      error
}

// runBenchmarkScenariosCollect runs each preset in sequence and returns the
// raw outcomes without printing a summary. One preset failing does NOT abort
// the others. Used by runBenchmarkScenarios (prints its own summary) and by
// runProxyShootout (aggregates across front-ends).
//
// proxy is the front-end label forwarded to forge (defaults to flagBenchProxy
// when empty). vip is the effective LLM host (defaults to flagBenchVIP when
// empty). Both are threaded through to runOnePreset → pushAiperfResult.
func runBenchmarkScenariosCollect(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	presets []benchmarkPreset,
	proxy, vip string,
) []scenarioOutcome {
	outcomes := make([]scenarioOutcome, 0, len(presets))
	for _, preset := range presets {
		outcome := runOnePreset(cmd, probOpts, creds, agentName, graph, preset, proxy, vip)
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

// runBenchmarkScenarios runs each preset in sequence, collecting outcomes.
// One preset failing does NOT abort the others. A summary table is printed
// at the end. Each preset is optionally registered as a forge BenchmarkConfig.
func runBenchmarkScenarios(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	presets []benchmarkPreset,
) error {
	// Non-shootout path: proxy and vip default to globals (empty strings).
	outcomes := runBenchmarkScenariosCollect(cmd, probOpts, creds, agentName, graph, presets, "", "")

	// Print summary table.
	fmt.Println()
	fmt.Printf("%-16s  %-10s  %-8s  %s\n", "PRESET", "STATUS", "RUN_ID", "NOTE")
	fmt.Printf("%-16s  %-10s  %-8s  %s\n", "------", "------", "------", "----")
	for _, o := range outcomes {
		note := ""
		if o.Err != nil {
			note = o.Err.Error()
			if len(note) > 60 {
				note = note[:57] + "..."
			}
		}
		fmt.Printf("%-16s  %-10s  %-8d  %s\n", o.Preset, o.Status, o.RunID, note)
	}

	// Return an error only if ALL presets failed.
	allFailed := true
	for _, o := range outcomes {
		if o.Err == nil {
			allFailed = false
			break
		}
	}
	if allFailed && len(outcomes) > 0 {
		return fmt.Errorf("all %d preset(s) failed", len(outcomes))
	}
	return nil
}

// runOnePreset executes a single preset: optionally registers the config,
// runs aiperf, and pushes the result to forge via the raw-JSON ingest path.
//
// proxy is the front-end label forwarded to forge (defaults to flagBenchProxy
// when empty). vip is the effective LLM host (defaults to flagBenchVIP when
// empty). Both are forwarded to pushAiperfResult so forge records the correct
// metadata for each front-end in a shootout.
func runOnePreset(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	preset benchmarkPreset,
	proxy, vip string,
) scenarioOutcome {
	label := presetRunLabel(flagBenchRunLabel, preset.Name)
	fmt.Fprintf(os.Stderr, "\n── preset: %s (%s) label=%s ──\n", preset.Name, preset.Description, label)

	// Effective VIP for config registration metadata (falls back to flagBenchVIP).
	effectiveVIP := vip
	if effectiveVIP == "" {
		effectiveVIP = flagBenchVIP
	}

	// Best-effort: register the preset as a forge BenchmarkConfig (idempotent).
	configJSON := map[string]any{
		"url":           llmBaseURL(effectiveVIP),
		"model":         flagBenchModel,
		"endpoint":      flagBenchEndpoint,
		"concurrency":   preset.Config.Concurrency,
		"request_count": preset.Config.NumRequests,
		"isl":           preset.Config.ISL,
		"osl":           preset.Config.OSL,
		"streaming":     preset.Config.Streaming,
	}
	cfgResp, cfgErr := registerBenchmarkConfigFn(cmd.Context(), forge.BenchmarkConfigOptions{
		RestURL:     flagBenchForgeURL,
		Creds:       creds,
		Name:        fmt.Sprintf("azrbnkctl-%s", preset.Name),
		Description: preset.Description,
		ConfigJSON:  configJSON,
	})
	presetConfigID := graph.configID // inherit from graph (0 if unset)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ forge config registration failed (non-fatal): %v\n", cfgErr)
	} else {
		fmt.Fprintf(os.Stderr, "✓ forge config registered: id=%d name=%s\n", cfgResp.ID, cfgResp.Name)
		presetConfigID = cfgResp.ID
	}

	// Build AiperfRunOptions from the preset, inheriting shared flags.
	cfg := preset.Config
	cfg.Model = flagBenchModel
	cfg.EndpointPath = flagBenchEndpoint
	cfg.Tokenizer = flagBenchTokenizer
	cfg.HostHeader = flagBenchHostHeader
	cfg.Timeout = flagBenchTimeout

	runOpts := jumphost.AiperfRunOptions{
		ProbeOptions: probOpts,
		Config:       cfg,
		RunLabel:     label,
	}

	fmt.Fprintf(os.Stderr, "→ Running aiperf (concurrency=%d requests=%d isl=%d osl=%d stream=%v)\n",
		cfg.Concurrency, cfg.NumRequests, cfg.ISL, cfg.OSL, cfg.Streaming)

	result, err := executeBenchmarkRun(cmd.Context(), runOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ preset %s aiperf failed: %v\n", preset.Name, err)
		return scenarioOutcome{Preset: preset.Name, Status: "FAILED", Err: err}
	}
	fmt.Fprintf(os.Stderr, "✓ aiperf done: %d/%d succeeded (%.1fs rps=%.3f)\n",
		result.Successful, result.TotalRequests, result.DurationSeconds, result.RequestThroughput)

	// Primary push path: raw JSON to the rich-ingest endpoint.
	// Pass proxy and vip so forge records the correct front-end label and URL.
	runID, pushErr := pushAiperfResult(cmd, result, creds, agentName, label, presetConfigID, graph.targetID, graph.proxyDeploymentID, proxy, vip, "", configJSON)
	if pushErr != nil {
		fmt.Fprintf(os.Stderr, "✗ preset %s forge push failed: %v\n", preset.Name, pushErr)
		return scenarioOutcome{Preset: preset.Name, Status: "PUSH_FAILED", Err: pushErr}
	}
	return scenarioOutcome{
		Preset:   preset.Name,
		RunID:    runID,
		ResultID: result.BaseURL,
		Status:   "OK",
	}
}

// pushAiperfResult is the shared raw-push + structured-fallback helper used by
// runOnePreset and runNativeScenario. It returns the forge run_id on success.
//
// proxyDeploymentID is stamped on both push paths (0 = unlinked / omitted).
// proxy is the label forwarded to forge (defaults to flagBenchProxy when empty).
// vip is the LLM base URL host; when empty flagBenchVIP is used.
// datasetName is optional — when non-empty it is forwarded as DatasetName on
// the raw push (used by the native scenario path to stamp <scenario key>).
// fallbackConfig is the map forwarded to the structured push path as AiperfConfig.
func pushAiperfResult(
	cmd *cobra.Command,
	result *jumphost.AiperfResult,
	creds forge.RestCreds,
	agentName, label string,
	configID, targetID, proxyDeploymentID int,
	proxy, vip string,
	datasetName string,
	fallbackConfig map[string]any,
) (int, error) {
	effectiveProxy := proxy
	if effectiveProxy == "" {
		effectiveProxy = flagBenchProxy
	}
	effectiveVIP := vip
	if effectiveVIP == "" {
		effectiveVIP = flagBenchVIP
	}

	rawPushOpts := forge.RawAiperfPushOptions{
		RestURL:           flagBenchForgeURL,
		Creds:             creds,
		RawJSON:           []byte(result.RawJSON),
		Proxy:             effectiveProxy,
		Model:             flagBenchModel,
		URL:               llmBaseURL(effectiveVIP),
		AgentName:         agentName,
		RunLabel:          label,
		TargetID:          targetID,
		ConfigID:          configID,
		ProxyDeploymentID: proxyDeploymentID,
		DatasetName:       datasetName,
	}

	rawResp, pushErr := pushRawAiperfResultFn(cmd.Context(), rawPushOpts)
	if pushErr != nil {
		// Fall back to structured push so a missing/old forge endpoint does not
		// abort a successful benchmark run.
		fmt.Fprintf(os.Stderr, "⚠ raw push failed (%v); falling back to structured push\n", pushErr)
		pushOpts := forge.BenchmarkPushOptions{
			RestURL:           flagBenchForgeURL,
			Creds:             creds,
			RunLabel:          label,
			Proxy:             effectiveProxy,
			AgentName:         agentName,
			AgentHostname:     flagBenchInstanceID,
			TargetID:          targetID,
			ConfigID:          configID,
			ProxyDeploymentID: proxyDeploymentID,
			AiperfConfig:      fallbackConfig,
		}
		structResp, structErr := pushBenchmarkResultFn(cmd.Context(), result, pushOpts)
		if structErr != nil {
			return 0, fmt.Errorf("raw+structured both failed: raw=%v structured=%w", pushErr, structErr)
		}
		fmt.Fprintf(os.Stderr, "✓ pushed (structured fallback): run_id=%d status=%s\n", structResp.RunID, structResp.Status)
		return structResp.RunID, nil
	}

	fmt.Fprintf(os.Stderr, "✓ pushed (raw): run_id=%d status=%s\n", rawResp.RunID, rawResp.Status)
	return rawResp.RunID, nil
}

// nativeScenarioOutcome records the result of a single child run within a
// native forge scenario.
type nativeScenarioOutcome struct {
	VariantLabel string
	RunID        int
	Status       string
	Err          error
}

// runNativeScenarioCollect expands a forge-native scenario key into its child
// runs, executes each, and returns the raw outcomes without printing a summary.
// Used by runNativeScenario (prints its own summary) and runProxyShootout
// (aggregates across front-ends).
//
// proxy is the front-end label forwarded to forge (defaults to flagBenchProxy
// when empty). vip is the effective LLM host (defaults to flagBenchVIP when
// empty). Both are forwarded to pushAiperfResult so forge records the correct
// metadata for each front-end in a shootout.
func runNativeScenarioCollect(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	scenarioKey string,
	scenarioConfigID int,
	proxy, vip string,
) ([]nativeScenarioOutcome, error) {
	baseCfg := jumphost.AiperfConfig{
		Model:        flagBenchModel,
		EndpointPath: flagBenchEndpoint,
		Tokenizer:    flagBenchTokenizer,
		HostHeader:   flagBenchHostHeader,
		Timeout:      flagBenchTimeout,
	}

	children, err := expandForgeScenario(scenarioKey, baseCfg)
	if err != nil {
		return nil, err
	}

	fmt.Fprintf(os.Stderr, "→ native scenario %q: %d child run(s)\n", scenarioKey, len(children))

	outcomes := make([]nativeScenarioOutcome, 0, len(children))

	for _, child := range children {
		label := presetRunLabel(flagBenchRunLabel, scenarioKey+"-"+child.VariantLabel)
		fmt.Fprintf(os.Stderr, "\n── child: %s label=%s ──\n", child.VariantLabel, label)

		cfg := child.Config
		if cfg.Scenario != "" {
			fmt.Fprintf(os.Stderr, "→ Running aiperf (scenario=%q dataset=%q request-rate=%g max-context=%d)\n",
				cfg.Scenario, cfg.PublicDataset, cfg.RequestRate, cfg.MaxContextLength)
		} else if cfg.SeqDist != "" {
			fmt.Fprintf(os.Stderr, "→ Running aiperf (concurrency=%d requests=%d seq-dist=%q stream=%v)\n",
				cfg.Concurrency, cfg.NumRequests, cfg.SeqDist, cfg.Streaming)
		} else {
			fmt.Fprintf(os.Stderr, "→ Running aiperf (concurrency=%d requests=%d isl=%d osl=%d stream=%v)\n",
				cfg.Concurrency, cfg.NumRequests, cfg.ISL, cfg.OSL, cfg.Streaming)
		}

		runOpts := jumphost.AiperfRunOptions{
			ProbeOptions: probOpts,
			Config:       cfg,
			RunLabel:     label,
		}

		result, aiperfErr := executeBenchmarkRun(cmd.Context(), runOpts)
		if aiperfErr != nil {
			fmt.Fprintf(os.Stderr, "✗ child %s aiperf failed: %v\n", child.VariantLabel, aiperfErr)
			outcomes = append(outcomes, nativeScenarioOutcome{
				VariantLabel: child.VariantLabel,
				Status:       "FAILED",
				Err:          aiperfErr,
			})
			continue
		}
		fmt.Fprintf(os.Stderr, "✓ aiperf done: %d/%d succeeded (%.1fs rps=%.3f)\n",
			result.Successful, result.TotalRequests, result.DurationSeconds, result.RequestThroughput)

		// Effective VIP for fallback config metadata (falls back to flagBenchVIP).
		effectiveVIP := vip
		if effectiveVIP == "" {
			effectiveVIP = flagBenchVIP
		}

		fallbackCfg := map[string]any{
			"scenario_key":  scenarioKey,
			"variant_label": child.VariantLabel,
			"url":           llmBaseURL(effectiveVIP),
			"model":         flagBenchModel,
			"concurrency":   cfg.Concurrency,
			"request_count": cfg.NumRequests,
			"streaming":     cfg.Streaming,
		}

		datasetName := scenarioKey
		pushLabel := label
		if scenarioKey == "mooncake" {
			datasetName = "mooncake-toolagent"
			if !strings.Contains(pushLabel, "tokenizer_substituted") {
				pushLabel = pushLabel + "+tokenizer_substituted"
			}
		}

		// Pass proxy and vip so forge records the correct front-end label and URL.
		runID, pushErr := pushAiperfResult(cmd, result, creds, agentName, pushLabel, scenarioConfigID, graph.targetID, graph.proxyDeploymentID, proxy, vip, datasetName, fallbackCfg)
		if pushErr != nil {
			fmt.Fprintf(os.Stderr, "✗ child %s push failed: %v\n", child.VariantLabel, pushErr)
			outcomes = append(outcomes, nativeScenarioOutcome{
				VariantLabel: child.VariantLabel,
				Status:       "PUSH_FAILED",
				Err:          pushErr,
			})
			continue
		}
		outcomes = append(outcomes, nativeScenarioOutcome{
			VariantLabel: child.VariantLabel,
			RunID:        runID,
			Status:       "OK",
		})
	}
	return outcomes, nil
}

// registerScenarioConfig registers one BenchmarkConfig for a scenario key and
// returns the effective configID (graph.configID when registration fails or the
// returned ID on success). Verbatim copy of the registration block from both
// runNativeScenario and runShootoutFrontEnd — extracted so the byte-identical
// stderr output is produced from a single place.
func registerScenarioConfig(
	ctx context.Context,
	creds forge.RestCreds,
	graph forgeGraph,
	key string,
) int {
	var description string
	if s, ok := forgeScenarioByKey(key); ok {
		description = s.Description
	}
	configJSON := forgeScenarioConfigJSON(key, flagBenchModel, flagBenchVIP)
	configName := fmt.Sprintf("azrbnkctl-scenario-%s", key)
	cfgResp, cfgErr := registerBenchmarkConfigFn(ctx, forge.BenchmarkConfigOptions{
		RestURL:     flagBenchForgeURL,
		Creds:       creds,
		Name:        configName,
		Description: description,
		ConfigJSON:  configJSON,
	})
	configID := graph.configID // inherit from graph (0 if unset)
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ forge config registration failed (non-fatal): %v\n", cfgErr)
	} else {
		fmt.Fprintf(os.Stderr, "✓ forge config registered: id=%d name=%s\n", cfgResp.ID, cfgResp.Name)
		configID = cfgResp.ID
	}
	return configID
}

// runScenarioSequence runs an ordered list of scenario keys in sequence,
// invoking resetCacheHookFn strictly between consecutive keys (never before
// the first, never after the last). It returns the collected outcomes and
// prints NOTHING — summary rendering is left to the callers.
//
// proxy is the front-end label (empty string in the single-scenario path).
// vip is the effective LLM host (empty string → callers fall back to
// flagBenchVIP inside runNativeScenarioCollect → pushAiperfResult).
func runScenarioSequence(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	keys []string,
	proxy, vip string,
) ([]nativeScenarioOutcome, error) {
	var allOutcomes []nativeScenarioOutcome
	for i, key := range keys {
		// Fire reset seam between consecutive scenarios (never before first).
		if i > 0 {
			prev := keys[i-1]
			if resetErr := resetCacheHookFn(cmd.Context(), resetCacheArgs{
				PrevKey:   prev,
				NextKey:   key,
				ProbeOpts: probOpts,
				Proxy:     proxy,
			}); resetErr != nil {
				return allOutcomes, &resetCacheError{PrevKey: prev, NextKey: key, cause: resetErr}
			}
		}

		configID := registerScenarioConfig(cmd.Context(), creds, graph, key)
		g := graph
		g.configID = configID

		outcomes, err := runNativeScenarioCollect(cmd, probOpts, creds, agentName, g, key, configID, proxy, vip)
		if err != nil {
			return allOutcomes, err
		}
		allOutcomes = append(allOutcomes, outcomes...)
	}
	return allOutcomes, nil
}

// runNativeScenario parses --scenario as an ordered comma-list of keys, validates
// all keys up front, then runs them in sequence via runScenarioSequence (which
// fires the reset seam between consecutive keys). A per-key summary table is
// printed after all keys complete. Returns an error only when all children of all
// keys failed, or when any key is unknown, or when the reset seam fails.
func runNativeScenario(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	rawScenario string,
) error {
	// Parse and validate all keys before any run starts (fail-fast, no partial run).
	keys, parseErr := parseScenarioKeys(rawScenario)
	if parseErr != nil {
		return parseErr
	}
	baseCfg := jumphost.AiperfConfig{
		Model:        flagBenchModel,
		EndpointPath: flagBenchEndpoint,
		Tokenizer:    flagBenchTokenizer,
		HostHeader:   flagBenchHostHeader,
		Timeout:      flagBenchTimeout,
	}
	for _, k := range keys {
		if _, ok := forgeScenarioByKey(k); !ok {
			return fmt.Errorf("--scenario: unknown key %q; valid keys: %s", k, strings.Join(forgeScenarioKeys(), ", "))
		}
		// Defense-in-depth: also verify expandForgeScenario resolves without error.
		if _, err := expandForgeScenario(k, baseCfg); err != nil {
			return fmt.Errorf("--scenario: key %q: %w", k, err)
		}
	}

	// Run the full sequence. Non-shootout path: proxy and vip are empty strings.
	allOutcomes, seqErr := runScenarioSequence(cmd, probOpts, creds, agentName, graph, keys, "", "")

	// Print per-key summary tables using child counts from expand (already
	// validated above, so re-expansion is purely in-memory bookkeeping).
	offset := 0
	for _, k := range keys {
		children, _ := expandForgeScenario(k, baseCfg) // error already checked above
		n := len(children)
		end := offset + n
		if end > len(allOutcomes) {
			end = len(allOutcomes)
		}
		keyOutcomes := allOutcomes[offset:end]
		offset = end

		// Skip printing a table for keys that never ran (e.g. scenario after a
		// reset failure). Their absence in the output is cleaner than an empty
		// header with no rows.
		if len(keyOutcomes) == 0 {
			continue
		}

		fmt.Println()
		fmt.Printf("scenario: %s\n", k)
		fmt.Printf("%-30s  %-10s  %-8s  %s\n", "VARIANT", "STATUS", "RUN_ID", "NOTE")
		fmt.Printf("%-30s  %-10s  %-8s  %s\n", "-------", "------", "------", "----")
		for _, o := range keyOutcomes {
			note := ""
			if o.Err != nil {
				note = o.Err.Error()
				if len(note) > 50 {
					note = note[:47] + "..."
				}
			}
			fmt.Printf("%-30s  %-10s  %-8d  %s\n", o.VariantLabel, o.Status, o.RunID, note)
		}
	}

	// Propagate a reset-seam error (already-run keys have been printed above).
	if seqErr != nil {
		return seqErr
	}

	// Fail only when all children across all keys failed.
	allFailed := true
	for _, o := range allOutcomes {
		if o.Err == nil {
			allFailed = false
			break
		}
	}
	if allFailed && len(allOutcomes) > 0 {
		return fmt.Errorf("all %d child run(s) failed across %d scenario(s)", len(allOutcomes), len(keys))
	}
	return nil
}

// ── Proxy shootout (WS-D1) ──────────────────────────────────────────────────

// shootoutFrontEnd describes one front-end in a proxy shootout.
type shootoutFrontEnd struct {
	// ProxyType is the forge canonical proxy type label (e.g. "envoy", "f5-bnk").
	ProxyType string
	// VIP overrides flagBenchVIP when non-empty (used by --direct-pod-ip and envoy NodePort).
	VIP string
	// ProxyDeploymentID is the resolved forge id (0 = unlinked).
	ProxyDeploymentID int
	// ResolveErr is set when endpoint resolution failed for this front-end.
	// runProxyShootout marks the leg RESOLVE_FAILED and continues other front-ends.
	ResolveErr error
}

// shootoutOutcome records the aggregated result for one front-end.
type shootoutOutcome struct {
	ProxyType         string
	ProxyDeploymentID int
	RunIDs            []int
	Status            string
	Err               error
}

// resolveShootoutFrontEnds builds the ordered list of front-ends from the
// --proxies CSV flag and --direct-pod-ip. It runs discover + list against
// forge to resolve ProxyDeploymentIDs (best-effort; 0 when unavailable).
//
// Returns an error immediately when any --proxies entry is not a recognised
// forge proxy type (e.g. a typo). Valid-but-undiscovered types (cluster has
// no running proxy of that kind) still proceed as unlinked front-ends.
//
// For non-f5-bnk proxy types: reads the front-end's reachable address from
// the discovered ProxyDeployment's external_url field (forge's "NodePort/LB
// URL for external agents", jumphost-reachable). When external_url is empty
// (discovery not yet populated, or LB ingress not provisioned) the front-end
// is returned with ResolveErr set — the caller marks it RESOLVE_FAILED. This
// is fail-closed: NEVER silently fall back to the global --vip (that would
// reintroduce the "silent BNK bake-off" bug). Other front-ends still run.
//
// f5-bnk: has no proxy Service so external_url is null by design. VIP stays
// empty and runProxyShootout falls back to the global --vip flag.
//
// The direct-pod-ip nodeport front-end is always appended last and is kept
// distinct from any VIP nodeport in --proxies.
func resolveShootoutFrontEnds(ctx context.Context, creds forge.RestCreds, targetID int) ([]shootoutFrontEnd, error) {
	var frontEnds []shootoutFrontEnd

	// Parse --proxies CSV.
	var proxyTypes []string
	for _, raw := range strings.Split(flagBenchProxies, ",") {
		t := strings.TrimSpace(raw)
		if t != "" {
			proxyTypes = append(proxyTypes, t)
		}
	}

	// Fail fast on unknown proxy type names (typo guard).
	for _, pt := range proxyTypes {
		if !forge.IsValidProxyType(pt) {
			return nil, fmt.Errorf("--proxies: unknown proxy type %q; valid types: envoy, f5-bnk, haproxy, nginx, nodeport", pt)
		}
	}

	// Best-effort: discover + list proxies from forge when we have a target.
	// Run when either --proxies is non-empty OR --direct-pod-ip is set so that
	// a nodeport-only direct-pod-ip run can still resolve its ProxyDeploymentID.
	var knownProxies []forge.ProxyDeployment
	if targetID != 0 && (len(proxyTypes) > 0 || flagBenchDirectPodIP != "") {
		discOpts := forge.ProxyDiscoverOptions{
			RestURL:  flagBenchForgeURL,
			Creds:    creds,
			TargetID: targetID,
		}
		if _, discErr := discoverProxiesFn(ctx, discOpts); discErr != nil {
			fmt.Fprintf(os.Stderr, "⚠ forge discover-proxies failed (non-fatal, running unlinked): %v\n", discErr)
		}
		listed, listErr := listProxyDeploymentsFn(ctx, discOpts)
		if listErr != nil {
			fmt.Fprintf(os.Stderr, "⚠ forge list-proxies failed (non-fatal, running unlinked): %v\n", listErr)
		} else {
			knownProxies = listed
		}
	}

	// Build VIP front-ends.
	for _, pt := range proxyTypes {
		dep := forge.FindProxyDeployment(knownProxies, pt)
		id := 0
		if dep != nil {
			id = dep.ID
		}
		fe := shootoutFrontEnd{
			ProxyType:         pt,
			ProxyDeploymentID: id,
		}

		// f5-bnk has no proxy Service → external_url is null by design.
		// Leave VIP empty; runProxyShootout falls back to the global --vip flag.
		//
		// All other proxy types read their jumphost-reachable address from
		// external_url. Fail-closed: an empty external_url (discovery not yet
		// run or LB ingress not provisioned) marks the leg RESOLVE_FAILED.
		// NEVER fall back to the global --vip (that silently benchmarks the BNK VIP).
		if pt != "f5-bnk" {
			if dep == nil || dep.ExternalURL == "" {
				if dep == nil {
					fe.ResolveErr = fmt.Errorf("%s front-end: no ProxyDeployment found in forge (run discover-proxies first)", pt)
				} else {
					fe.ResolveErr = fmt.Errorf("%s front-end: external_url is empty (forge discovery has not populated it yet; check Service type and LB ingress)", pt)
				}
				fmt.Fprintf(os.Stderr, "⚠ %v\n", fe.ResolveErr)
			} else {
				fe.VIP = dep.ExternalURL
				fmt.Fprintf(os.Stderr, "✓ %s endpoint resolved from forge external_url: %s\n", pt, fe.VIP)
			}
		}

		frontEnds = append(frontEnds, fe)
	}

	// Append direct-pod-ip nodeport front-end (always last, always distinct).
	if flagBenchDirectPodIP != "" {
		// Resolve the proxy_deployment_id for "nodeport" from the same list
		// (best-effort; may be different from any VIP nodeport front-end above,
		// or the same id if there is only one nodeport record).
		id := forge.ResolveProxyDeploymentID(knownProxies, "nodeport")
		frontEnds = append(frontEnds, shootoutFrontEnd{
			ProxyType:         "nodeport",
			VIP:               flagBenchDirectPodIP,
			ProxyDeploymentID: id,
		})
	}

	return frontEnds, nil
}

// runProxyShootout orchestrates the proxy shootout: runs the chosen
// --scenario (or presets / single-run) once per front-end, stamping
// the per-front-end proxy_deployment_id on every push, then prints a
// summary table with real run_ids from each front-end.
//
// One front-end failing does NOT abort the others (mirrors runBenchmarkScenarios).
func runProxyShootout(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	presets []benchmarkPreset,
) error {
	frontEnds, err := resolveShootoutFrontEnds(cmd.Context(), creds, graph.targetID)
	if err != nil {
		return err
	}
	if len(frontEnds) == 0 {
		return fmt.Errorf("--proxies is empty and --direct-pod-ip is not set; nothing to shoot out")
	}

	fmt.Fprintf(os.Stderr, "→ proxy shootout: %d front-end(s)\n", len(frontEnds))

	outcomes := make([]shootoutOutcome, 0, len(frontEnds))

	for _, fe := range frontEnds {
		// Fail-closed: a front-end whose endpoint resolution failed is marked
		// RESOLVE_FAILED immediately — never runs, never touches the default VIP.
		if fe.ResolveErr != nil {
			outcomes = append(outcomes, shootoutOutcome{
				ProxyType:         fe.ProxyType,
				ProxyDeploymentID: fe.ProxyDeploymentID,
				Status:            "RESOLVE_FAILED",
				Err:               fe.ResolveErr,
			})
			continue
		}

		// Copy graph with the per-front-end proxy id; never mutate globals.
		g := graph
		g.proxyDeploymentID = fe.ProxyDeploymentID

		// When the front-end has a specific VIP (direct-pod-ip or external_url),
		// override the probe VIP for this front-end's runs.
		feProbeOpts := probOpts
		if fe.VIP != "" {
			feProbeOpts.VIP = fe.VIP
		}

		linked := "unlinked"
		if fe.ProxyDeploymentID != 0 {
			linked = fmt.Sprintf("id=%d", fe.ProxyDeploymentID)
		}
		fmt.Fprintf(os.Stderr, "\n══ front-end: %s (%s) vip=%s ══\n",
			fe.ProxyType, linked, feProbeOpts.VIP)

		oc := runShootoutFrontEnd(cmd, feProbeOpts, creds, agentName, g, fe.ProxyType, fe.VIP, presets)
		outcomes = append(outcomes, oc)
	}

	// Outer summary table.
	fmt.Println()
	fmt.Printf("proxy shootout summary\n")
	fmt.Printf("%-14s  %-12s  %-8s  %-8s  %s\n", "PROXY", "PROXY_DEP_ID", "STATUS", "RUN_IDS", "NOTE")
	fmt.Printf("%-14s  %-12s  %-8s  %-8s  %s\n", "-----", "------------", "------", "-------", "----")
	for _, o := range outcomes {
		depID := "unlinked"
		if o.ProxyDeploymentID != 0 {
			depID = fmt.Sprintf("%d", o.ProxyDeploymentID)
		}
		runIDs := formatRunIDs(o.RunIDs)
		note := ""
		if o.Err != nil {
			note = o.Err.Error()
			if len(note) > 50 {
				note = note[:47] + "..."
			}
		}
		fmt.Printf("%-14s  %-12s  %-8s  %-8s  %s\n", o.ProxyType, depID, o.Status, runIDs, note)
	}

	// Fail only when all front-ends failed.
	allFailed := true
	for _, o := range outcomes {
		if o.Err == nil {
			allFailed = false
			break
		}
	}
	if allFailed && len(outcomes) > 0 {
		return fmt.Errorf("all %d front-end(s) failed in proxy shootout", len(outcomes))
	}
	return nil
}

// runShootoutFrontEnd runs one front-end's workload (native scenario, presets,
// or single run) and returns a shootoutOutcome with the collected run_ids.
// The proxy label and VIP are taken from proxyType and vip (not from globals).
func runShootoutFrontEnd(
	cmd *cobra.Command,
	probOpts jumphost.ProbeOptions,
	creds forge.RestCreds,
	agentName string,
	graph forgeGraph,
	proxyType, vip string,
	presets []benchmarkPreset,
) shootoutOutcome {
	base := shootoutOutcome{ProxyType: proxyType, ProxyDeploymentID: graph.proxyDeploymentID}

	// Per-front-end served-model preflight.  Each front-end may target a
	// different VIP (e.g. nodeport direct-pod-ip), so we preflight here with
	// the effective per-front-end probOpts (which already has VIP set by the
	// caller for direct-pod-ip front-ends).
	fmt.Fprintf(os.Stderr, "→ Preflight: checking model %q at vip=%s (front-end=%s)\n",
		flagBenchModel, probOpts.VIP, proxyType)
	if pfErr := checkServedModelFn(cmd.Context(), probOpts, flagBenchModel); pfErr != nil {
		base.Status = "PREFLIGHT_FAILED"
		base.Err = fmt.Errorf("served-model preflight: %w", pfErr)
		return base
	}

	switch {
	case flagBenchScenario != "":
		// Native scenario mode: parse keys (already validated in runForgeBenchmark
		// via runNativeScenario; defensive parse here for the shootout path which
		// calls runShootoutFrontEnd directly).
		keys, parseErr := parseScenarioKeys(flagBenchScenario)
		if parseErr != nil {
			base.Status = "FAILED"
			base.Err = parseErr
			return base
		}
		// Run all scenario keys in sequence via the shared helper.
		// Pass proxyType and vip so forge records correct per-front-end metadata.
		// resetCacheHookFn fires between keys inside runScenarioSequence.
		children, seqErr := runScenarioSequence(cmd, probOpts, creds, agentName, graph, keys, proxyType, vip)
		if seqErr != nil {
			// Check whether this is a cache-reset failure — surface as RESET_FAILED
			// so other front-ends still run (mirrors FAILED / RESOLVE_FAILED pattern).
			var rce *resetCacheError
			if errors.As(seqErr, &rce) {
				base.Status = "RESET_FAILED"
				base.Err = seqErr
				return base
			}
			base.Status = "FAILED"
			base.Err = seqErr
			return base
		}
		var runIDs []int
		allFailed := true
		for _, ch := range children {
			if ch.Err == nil {
				allFailed = false
				if ch.RunID != 0 {
					runIDs = append(runIDs, ch.RunID)
				}
			}
		}
		base.RunIDs = runIDs
		if allFailed && len(children) > 0 {
			base.Status = "FAILED"
			base.Err = fmt.Errorf("all %d child run(s) failed", len(children))
		} else {
			base.Status = "OK"
		}
		return base

	case len(presets) > 0:
		// Preset mode: collect per-preset outcomes.
		// Pass proxyType and vip so forge records correct per-front-end metadata.
		presOutcomes := runBenchmarkScenariosCollect(cmd, probOpts, creds, agentName, graph, presets, proxyType, vip)
		var runIDs []int
		allFailed := true
		for _, o := range presOutcomes {
			if o.Err == nil {
				allFailed = false
				if o.RunID != 0 {
					runIDs = append(runIDs, o.RunID)
				}
			}
		}
		base.RunIDs = runIDs
		if allFailed && len(presOutcomes) > 0 {
			base.Status = "FAILED"
			base.Err = fmt.Errorf("all %d preset(s) failed", len(presOutcomes))
		} else {
			base.Status = "OK"
		}
		return base

	default:
		// Single-run mode: run aiperf and push.
		effectiveVIP := vip
		if effectiveVIP == "" {
			effectiveVIP = flagBenchVIP
		}
		runOpts := jumphost.AiperfRunOptions{
			ProbeOptions: probOpts,
			Config: jumphost.AiperfConfig{
				Model:        flagBenchModel,
				EndpointPath: flagBenchEndpoint,
				Concurrency:  flagBenchConcurrency,
				NumRequests:  flagBenchNumRequests,
				ISL:          flagBenchISL,
				OSL:          flagBenchOSL,
				Streaming:    flagBenchStreaming,
				Tokenizer:    flagBenchTokenizer,
				HostHeader:   flagBenchHostHeader,
				Timeout:      flagBenchTimeout,
			},
			RunLabel: flagBenchRunLabel,
			ResultID: flagBenchResultID,
		}
		result, aiperfErr := executeBenchmarkRun(cmd.Context(), runOpts)
		if aiperfErr != nil {
			base.Status = "FAILED"
			base.Err = aiperfErr
			return base
		}
		fmt.Fprintf(os.Stderr, "✓ aiperf done: %d/%d succeeded (%.1fs rps=%.3f)\n",
			result.Successful, result.TotalRequests, result.DurationSeconds, result.RequestThroughput)
		runID, pushErr := pushAiperfResult(
			cmd, result, creds, agentName, flagBenchRunLabel,
			graph.configID, graph.targetID, graph.proxyDeploymentID,
			proxyType, effectiveVIP,
			"", nil,
		)
		if pushErr != nil {
			base.Status = "PUSH_FAILED"
			base.Err = pushErr
			return base
		}
		base.RunIDs = []int{runID}
		base.Status = "OK"
		return base
	}
}

// llmBaseURL returns the LLM base URL for a front-end host. When host already
// carries an http:// or https:// scheme (case-insensitive) it is returned
// unchanged; otherwise "http://" is prepended. This tolerates forge's
// external_url, which for non-BNK front-ends may already include a scheme
// (e.g. "http://10.0.10.247:10080") and would otherwise yield "http://http://…".
func llmBaseURL(host string) string {
	lower := strings.ToLower(host)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return host
	}
	return "http://" + host
}

// formatRunIDs formats a slice of run ids for display (e.g. "42,43,44").
// Returns "-" when the slice is empty.
func formatRunIDs(ids []int) string {
	if len(ids) == 0 {
		return "-"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("%d", id)
	}
	return strings.Join(parts, ",")
}
