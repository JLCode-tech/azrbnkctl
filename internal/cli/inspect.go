package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericiooptions"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/forge"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/bnkscan"
)

var (
	flagFollow         bool
	flagLogsNamespace  string
	flagLogsContainer  string
	flagLogsPrevious   bool
	flagLogsSince      string
	flagLogsTailLines  int64
	flagLogsGovernance bool
	flagLogsMCP        bool

	// flagStatusConfig points status at a cluster.yaml so it can locate
	// the Azure phased path's state.env IDs cache
	// (.azrbnkctl/<metadata.name>/state.env). When unset, status falls
	// back to ./cluster.yaml under the working directory.
	flagStatusConfig string
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Summary of the workspace: cluster, components, deploy state",
	Long: `azrbnkctl status reports a quick read of the workspace:

  - configured cluster name + location + subscription
  - deploy state read from the Azure phased path's state.env IDs cache
    (.azrbnkctl/<cluster>/state.env): VNet, AKS cluster, TMM node pool,
    TMM node / jumphost, BNK activation, forge link, last phase applied
  - kubeconfig path (if any)
  - cluster reachability (node count + ready count)
  - BNK API generation (2.4, 2.3, mixed, none), Infra Programmed, Gateway
    Accepted+Programmed, CNE controller rollout and the readiness verdict
    (the same bnkscan index forge scan and doctor use)

Pass --config <cluster.yaml> to point status at a specific cluster's
state.env. Without it, status looks for ./cluster.yaml under the working
directory.

Every section is best-effort: a missing or unreadable state.env degrades
to "not deployed" rather than failing the command.`,
	RunE: runStatus,
}

var logsCmd = &cobra.Command{
	Use:   "logs <component>",
	Short: "Tail logs for a BNK component (flo, cwc, cis, cne, cert-manager, cneinstance, bgp, tmm)",
	Long: `Looks up the named BNK component, finds its pod(s) by label, and
streams logs to stdout. With --follow, streams live. With multiple
matching pods, tails the first and prints a hint about using
azrbnkctl k for per-pod selection.

The component → namespace/selector map is hardcoded against the
upstream chart's default labels (f5-cne-core and f5-cne-system); if your
install renamed namespaces or relabelled, fall back to:

  azrbnkctl k logs -n <ns> <pod>

--governance filters the stream to the BNKGOV records the governance iRule
writes (tmm component, f5-fluentbit sidecar by default) and prints one line
per decision:

  [GOV] <status> <rpc_method> tool=<tool> session=<session> latency=<ms>ms action=<action>

--mcp keeps only records with an MCP rpc_method (initialize, tools/list,
tools/call). Both work with --follow.`,
	Args: cobra.ExactArgs(1),
	RunE: runLogs,
}

func init() {
	statusCmd.Flags().StringVarP(&flagStatusConfig, "config", "f", "", "path to cluster.yaml (locates the phased path's state.env; defaults to ./cluster.yaml)")
	logsCmd.Flags().BoolVarP(&flagFollow, "follow", "f", false, "follow log output")
	logsCmd.Flags().StringVarP(&flagLogsNamespace, "namespace", "n", "", "override the component's default namespace")
	logsCmd.Flags().StringVarP(&flagLogsContainer, "container", "c", "", "container name in a multi-container pod")
	logsCmd.Flags().BoolVar(&flagLogsPrevious, "previous", false, "fetch logs from the previous container instance")
	logsCmd.Flags().StringVar(&flagLogsSince, "since", "", "only return logs newer than this duration (e.g. 5s, 2m, 1h)")
	logsCmd.Flags().Int64Var(&flagLogsTailLines, "tail", -1, "tail the last N lines (-1 = full log)")
	logsCmd.Flags().BoolVar(&flagLogsGovernance, "governance", false, "print only BNKGOV governance records, one formatted line each (tmm component)")
	logsCmd.Flags().BoolVar(&flagLogsMCP, "mcp", false, "like --governance, but only records of MCP JSON-RPC calls (rpc_method set)")
	rootCmd.AddCommand(statusCmd, logsCmd)
}

func runStatus(cmd *cobra.Command, _ []string) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	defer tw.Flush()

	cfgPath := flagStatusConfig
	if cfgPath == "" {
		if _, err := os.Stat("cluster.yaml"); err == nil {
			cfgPath = "cluster.yaml"
		}
	}

	var cl *intent.Cluster
	if cfgPath != "" {
		var err error
		cl, err = intent.Load(cfgPath)
		if err != nil && flagStatusConfig != "" {
			return fmt.Errorf("loading --config %s: %w", flagStatusConfig, err)
		}
	}

	clusterName := ""
	switch {
	case cl != nil:
		fmt.Fprintf(tw, "Config:\t%s\n", cfgPath)
		fmt.Fprintf(tw, "Subscription:\t%s\n", or(cl.Metadata.SubscriptionID, "(unset)"))
		fmt.Fprintf(tw, "Location:\t%s\n", or(cl.Metadata.Location, "(unset)"))
		fmt.Fprintf(tw, "Cluster:\t%s\n", or(cl.Metadata.Name, "(unset)"))
		clusterName = cl.Metadata.Name
	default:
		fmt.Fprintln(tw, "Status:\t(not initialised — run `azrbnkctl init`, or pass --config <cluster.yaml>)")
		return nil
	}

	st, _ := loadStatusState(cfgPath, clusterName)
	writeStatusDemoBanner(tw, st)
	writeStatusDeployStateFromState(tw, st)

	kcPath := resolveKubeconfigForStatus(st)
	if kcPath == "" {
		fmt.Fprintln(tw, "Kubeconfig:\t(none — run `azrbnkctl kubeconfig -f <cluster.yaml>`)")
		return nil
	}
	fmt.Fprintf(tw, "Kubeconfig:\t%s\n", kcPath)

	_ = tw.Flush()

	clusterStatus := probeCluster(cmd.Context(), kcPath)
	fmt.Fprintf(os.Stdout, "Cluster:        %s\n", clusterStatus)

	idx, scanErr := scanBNK(cmd.Context(), kcPath, bnkscan.DefaultControllerNamespace, statusScanTimeout)
	tw2 := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	writeStatusBNK(tw2, idx, scanErr)
	return tw2.Flush()
}

// writeStatusDemoBanner emits the demo banner and a warn-only expiry notice
// when the cluster is a demo deployment (DEMO_MODE=true in state).
func writeStatusDemoBanner(tw io.Writer, st *state.State) {
	if st == nil || st.Get("DEMO_MODE") != "true" {
		return
	}
	fmt.Fprintln(tw, "⚠ DEMO:\tnot a production deployment")
	exp := st.Get("DEMO_EXPIRY")
	if exp == "" {
		return
	}
	t, err := time.Parse(time.RFC3339, exp)
	if err != nil {
		fmt.Fprintf(tw, "Demo expires:\t%s (unparseable)\n", exp)
		return
	}
	remaining := time.Until(t)
	if remaining > 0 {
		fmt.Fprintf(tw, "Demo expires:\t%s (in %s)\n", exp, humanizeDuration(remaining))
	} else {
		fmt.Fprintf(tw, "Demo expires:\t%s (EXPIRED)\n", exp)
	}
}

func humanizeDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// writeStatusDeployStateFromState emits the deploy-state lines for `status`
// from an already-loaded *state.State. When st is nil (state not found or
// unreadable) it emits a single "no state" hint line.
func writeStatusDeployStateFromState(tw io.Writer, st *state.State) {
	if st == nil {
		fmt.Fprintln(tw, "Deploy state:\t(no state — run `azrbnkctl up --config <cluster.yaml>`)")
		return
	}

	if rg := st.Get("AZURE_RESOURCE_GROUP"); rg != "" {
		fmt.Fprintf(tw, "Resource group:\t%s\n", rg)
	}
	fmt.Fprintf(tw, "VNet:\t%s\n", orNotProvisioned(firstNonEmpty(st.Get("AZURE_VNET_NAME"), st.Get("AZURE_VNET_ID"), st.Get("VPC_ID"))))
	fmt.Fprintf(tw, "AKS cluster:\t%s\n", orNotProvisioned(firstNonEmpty(st.Get("AZURE_AKS_CLUSTER_NAME"), st.Get("EKS_CLUSTER_NAME"))))
	fmt.Fprintf(tw, "TMM node pool:\t%s\n", orNotProvisioned(firstNonEmpty(st.Get("AZURE_TMM_POOL_NAME"), st.Get("NODEGROUP_DEFAULT_NAME"))))
	fmt.Fprintf(tw, "TMM node:\t%s\n", tmmNodeLine(st))
	fmt.Fprintf(tw, "Jumphost:\t%s\n", orNotProvisioned(firstNonEmpty(st.Get("AZURE_JUMPHOST_VM_NAME"), st.Get("JUMPHOST_INSTANCE_NAME"), st.Get("JUMPHOST_INSTANCE_ID"))))
	fmt.Fprintf(tw, "BNK activation:\t%s\n", bnkActivationLine(st))
	fmt.Fprintf(tw, "Forge:\t%s\n", forgeLine(st))
	fmt.Fprintf(tw, "Last phase applied:\t%s\n", lastPhaseAppliedLine(st))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveKubeconfigForStatus returns the kubeconfig path to use for the status
// command's cluster probe.
func resolveKubeconfigForStatus(st *state.State) string {
	if st != nil {
		if stored := st.Get("KUBECONFIG_PATH"); stored != "" {
			if _, err := os.Stat(stored); err == nil {
				return stored
			}
			candidate := filepath.Join(st.Dir(), filepath.Base(stored))
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	return k8s.DefaultKubeconfigPath()
}

// loadStatusState resolves the cluster name then loads its state.env.
func loadStatusState(configPath, fallbackClusterName string) (*state.State, bool) {
	var stateDir string
	switch {
	case configPath != "":
		cl, err := intent.Load(configPath)
		if err != nil || cl.Metadata.Name == "" {
			return nil, false
		}
		stateDir = cl.StateDir()
	case fallbackClusterName != "":
		stateDir = filepath.Join(".azrbnkctl", fallbackClusterName)
	default:
		return nil, false
	}

	st, err := state.Load(stateDir)
	if err != nil {
		return nil, false
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "state.env")); statErr != nil {
		return nil, false
	}
	return st, true
}

func tmmNodeLine(st *state.State) string {
	if name := firstNonEmpty(st.Get("AZURE_TMM_NODE_NAME"), st.Get("TMM_NODE_NAME")); name != "" {
		return name
	}
	return orNotProvisioned(st.Get("TMM_INSTANCE_ID"))
}

func bnkActivationLine(st *state.State) string {
	ready := st.Get("CNEINSTANCE_READY_AT")
	licenseApplied := st.Get("LICENSE_APPLIED_AT") != "" || st.Get("LICENSE_NAME") != ""
	switch {
	case ready != "" && licenseApplied:
		return fmt.Sprintf("Active (license %s, ready %s)", orUnnamed(st.Get("LICENSE_NAME")), ready)
	case ready != "":
		return fmt.Sprintf("Active (ready %s)", ready)
	case licenseApplied:
		return fmt.Sprintf("activating (license %s applied, CNEInstance not yet Ready)", orUnnamed(st.Get("LICENSE_NAME")))
	default:
		return "not activated"
	}
}

func forgeLine(st *state.State) string {
	status := st.Get("FORGE_STATUS")
	if status == "" && st.Get("FORGE_REGISTERED") == "true" {
		status = "registered"
	}
	project := st.Get("FORGE_PROJECT_ID")
	switch {
	case status == "" && project == "":
		return "not linked"
	case project != "":
		return fmt.Sprintf("%s (project %s)", orUnknown(status), project)
	default:
		return orUnknown(status)
	}
}

var statusTimestampKeys = []string{
	"CLOUD_NETWORK_MAPPING_APPLIED_AT",
	"IRSA_SA_APPLIED_AT",
	"NADS_APPLIED_AT",
	"IFACE_DISCOVERY_AT",
	"F5SPKVLAN_APPLIED_AT",
	"FLO_INSTALLED_AT",
	"HUGEPAGES_DS_INSTALLED_AT",
	"DSSM_INSECURE_OVERLAY_APPLIED_AT",
	"TMM_LOG_STREAM_ENABLED_AT",
	"LICENSE_CRD_READY_AT",
	"LICENSE_APPLIED_AT",
	"CNEINSTANCE_APPLIED_AT",
	"CNEINSTANCE_RECONCILE_STARTED_AT",
	"CNEINSTANCE_READY_AT",
}

func lastPhaseAppliedLine(st *state.State) string {
	var bestKey string
	var bestTime time.Time

	checkKey := func(k, v string) {
		if v == "" {
			return
		}
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return
		}
		if t.After(bestTime) || (t.Equal(bestTime) && (bestKey == "" || strings.HasPrefix(bestKey, "PHASE_"))) {
			bestTime = t
			bestKey = k
		}
	}

	for _, k := range statusTimestampKeys {
		checkKey(k, st.Get(k))
	}
	for k, v := range st.All() {
		if strings.HasPrefix(k, "PHASE_") && strings.HasSuffix(k, "_APPLIED_AT") {
			checkKey(k, v)
		}
	}

	if bestKey == "" {
		return "(no phase timestamps yet — run `azrbnkctl up --config <cluster.yaml>`)"
	}
	return fmt.Sprintf("%s at %s", bestKey, bestTime.Format("2006-01-02 15:04:05 MST"))
}

func probeCluster(ctx context.Context, kubeconfigPath string) string {
	cs, err := k8s.BuildClientset(kubeconfigPath)
	if err != nil {
		return fmt.Sprintf("(unreachable: %v)", err)
	}
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	nodes, err := cs.CoreV1().Nodes().List(tctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Sprintf("(unreachable: %v)", err)
	}
	ready := 0
	for i := range nodes.Items {
		if nodeReady(&nodes.Items[i]) {
			ready++
		}
	}
	if len(nodes.Items) == 0 {
		return "0 nodes (unusual — check cluster)"
	}
	return fmt.Sprintf("%d/%d nodes ready", ready, len(nodes.Items))
}

func nodeReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// ── small formatters ─────────────────────────────────────────────────

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orNotProvisioned(s string) string { return or(s, "not provisioned") }

func orUnnamed(s string) string { return or(s, "unnamed") }

func orUnknown(s string) string { return or(s, "unknown") }

// ── logs ─────────────────────────────────────────────────────────────

type bnkComponent struct {
	Name      string
	Desc      string
	Ns        string
	Selector  string
	Container string
}

var bnkComponents = []bnkComponent{
	{"flo", "F5 Lifecycle Operator", "f5-cne-core", "app.kubernetes.io/name=f5-lifecycle-operator", ""},
	{"cwc", "F5 SPK Cluster Wide Controller", "f5-cne-core", "app=f5-spk-cwc", ""},
	{"cis", "F5 BNK CIS controller", "f5-cne-system", "app=f5-bnk-cis", ""},
	{"cne", "F5 CNE controller", "f5-cne-system", "app=f5-cne-controller", "f5-cne-controller"},
	{"cert-manager", "cert-manager", "cert-manager", "app.kubernetes.io/instance=cert-manager", ""},
	{"cneinstance", "BIG-IP TMM data plane (CNEInstance pods)", "f5-cne-system", "app=f5-tmm", "f5-tmm"},
	{"bgp", "BIG-IP TMM BGP routing container", "f5-cne-system", "app=f5-tmm", "f5-tmm-routing"},
	{"tmm", "BIG-IP TMM data plane (TMM log stream on f5-toda-fluentd stdout)", k8s.TMMLogNamespace, k8s.TMMLogPodSelector, ""},
}

func runLogs(cmd *cobra.Command, args []string) error {
	component := args[0]
	comp := lookupComponent(component)

	var out io.Writer = os.Stdout
	var gov *governanceFilter
	if flagLogsGovernance || flagLogsMCP {
		gov = newGovernanceFilter(os.Stdout, flagLogsMCP)
		out = gov
		defer func() {
			gov.Flush()
			fmt.Fprintf(os.Stderr, "→ %d governance record(s), %d other line(s) dropped\n", gov.Records, gov.Dropped)
		}()
		if comp != nil && comp.Name == "tmm" && flagLogsContainer == "" {
			flagLogsContainer = tmmLogContainer
		}
	}
	var tmmFilter *tmmLineFilter
	if comp != nil && comp.Name == "tmm" && gov == nil {
		tmmFilter = &tmmLineFilter{w: out}
		out = tmmFilter
		defer tmmFilter.Flush()
		if flagLogsContainer == "" {
			flagLogsContainer = tmmLogContainer
		}
	}
	if comp == nil {
		since, err := k8s.ParseSinceDuration(flagLogsSince)
		if err != nil {
			return err
		}
		opts := &k8s.LogsOptions{
			PodName:      component,
			Namespace:    flagLogsNamespace,
			Container:    flagLogsContainer,
			Follow:       flagFollow,
			Previous:     flagLogsPrevious,
			SinceSeconds: since,
			TailLines:    flagLogsTailLines,
			IOStreams: genericiooptions.IOStreams{
				In:     os.Stdin,
				Out:    out,
				ErrOut: os.Stderr,
			},
		}
		if err := opts.Run(cmd.Context()); err != nil {
			names := make([]string, 0, len(bnkComponents))
			for _, c := range bnkComponents {
				names = append(names, c.Name)
			}
			return fmt.Errorf("%w (also not a known BNK component: %s)",
				err, strings.Join(names, ", "))
		}
		return nil
	}

	cs, err := k8s.BuildClientset("")
	if err != nil {
		return err
	}

	ns := comp.Ns
	if comp.Name == "tmm" {
		if on, err := k8s.TMMLogStreamEnabled(cmd.Context(), cs); err == nil && !on {
			return fmt.Errorf("the TMM log stream is off: the %s/%s ConfigMap has no `@type stdout` store; run `azrbnkctl bnk heal` to enable it", k8s.TMMLogNamespace, k8s.TMMLogCustomConfigMap)
		}
	}
	if flagLogsNamespace != "" {
		ns = flagLogsNamespace
	}
	container := flagLogsContainer
	if container == "" && comp.Container != "" {
		container = comp.Container
	}

	ctx := cmd.Context()
	pods, err := cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: comp.Selector,
	})
	if err != nil {
		return fmt.Errorf("listing %s pods in %s: %w", component, ns, err)
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("no %s pods found in namespace %s with selector %s — chart label may have changed; try `azrbnkctl k get pods -A | grep <name>`",
			component, ns, comp.Selector)
	}
	pod := &pods.Items[0]
	if len(pods.Items) > 1 {
		fmt.Fprintf(os.Stderr, "→ %d %s pods found; tailing %s (use `azrbnkctl k logs -n %s <pod>` for a specific one)\n",
			len(pods.Items), component, pod.Name, pod.Namespace)
	} else {
		fmt.Fprintf(os.Stderr, "→ Tailing logs from %s/%s\n", pod.Namespace, pod.Name)
	}

	since, err := k8s.ParseSinceDuration(flagLogsSince)
	if err != nil {
		return err
	}
	logOpts := &corev1.PodLogOptions{
		Container: container,
		Follow:    flagFollow,
		Previous:  flagLogsPrevious,
	}
	if since > 0 {
		logOpts.SinceSeconds = &since
	}
	if flagLogsTailLines >= 0 {
		t := flagLogsTailLines
		logOpts.TailLines = &t
	}

	req := cs.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("opening log stream: %w", err)
	}
	defer stream.Close()
	_, err = io.Copy(out, stream)
	return err
}

func lookupComponent(name string) *bnkComponent {
	for i := range bnkComponents {
		if bnkComponents[i].Name == name {
			return &bnkComponents[i]
		}
	}
	return nil
}

// ── governance stream filter ─────────────────────────────────────────

const tmmLogContainer = k8s.TMMLogContainer

type tmmLineFilter struct {
	w   io.Writer
	buf []byte
}

func (f *tmmLineFilter) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	for {
		i := bytes.IndexByte(f.buf, '\n')
		if i < 0 {
			break
		}
		line := string(f.buf[:i])
		f.buf = f.buf[i+1:]
		if err := f.emit(line); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (f *tmmLineFilter) Flush() {
	if len(f.buf) > 0 {
		_ = f.emit(string(f.buf))
		f.buf = nil
	}
}

func (f *tmmLineFilter) emit(line string) error {
	if !strings.Contains(line, k8s.TMMLogPodMarker) {
		return nil
	}
	_, err := fmt.Fprintln(f.w, tmmLogText(line))
	return err
}

func tmmLogText(line string) string {
	i := strings.Index(line, "{")
	if i < 0 {
		return line
	}
	var rec struct {
		Log string `json:"log"`
	}
	if err := json.Unmarshal([]byte(line[i:]), &rec); err != nil || rec.Log == "" {
		return line
	}
	return strings.TrimRight(rec.Log, "\n")
}

type governanceFilter struct {
	w       io.Writer
	buf     []byte
	mcpOnly bool
	Records int
	Dropped int
}

func newGovernanceFilter(w io.Writer, mcpOnly bool) *governanceFilter {
	return &governanceFilter{w: w, mcpOnly: mcpOnly}
}

func (g *governanceFilter) Write(p []byte) (int, error) {
	g.buf = append(g.buf, p...)
	for {
		i := bytes.IndexByte(g.buf, '\n')
		if i < 0 {
			break
		}
		line := g.buf[:i]
		g.buf = g.buf[i+1:]
		if err := g.emit(string(line)); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

func (g *governanceFilter) Flush() {
	if len(g.buf) > 0 {
		_ = g.emit(string(g.buf))
		f := g
		f.buf = nil
	}
}

func (g *governanceFilter) emit(line string) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	rec, err := forge.ParseGovernanceLine(line)
	if err != nil || (g.mcpOnly && rec.RPCMethod == "") {
		g.Dropped++
		return nil
	}
	g.Records++
	_, werr := fmt.Fprintln(g.w, formatGovernance(rec))
	return werr
}

func formatGovernance(rec forge.GovernanceRecord) string {
	return fmt.Sprintf("[GOV] %s %s tool=%s session=%s latency=%dms action=%s",
		or(rec.Status, "-"), or(rec.RPCMethod, "-"), or(rec.Tool, "-"), or(rec.Session, "-"), rec.LatencyMS, or(rec.Action, "-"))
}
