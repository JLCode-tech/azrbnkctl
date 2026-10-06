package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/forge"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/bnkscan"
)

// Flags of forge scan (the discovery flags are shared, see addScanFlags).
var (
	flagForgeScanRegister bool
	flagForgeScanRemote   bool
)

// Seams replaced by tests.
var (
	forgeScanProbe = bnkscan.ProbeTools
	// forgeScanLink finds the registered forge link for the cluster: the
	// cluster.yaml state dir, else the default state dir.
	forgeScanLink = func(cl *intent.Cluster) *forge.Link {
		linkDir := ""
		if cl != nil {
			linkDir = cl.StateDir()
		} else if flagForgeConfigFile != "" {
			if loaded, err := intent.Load(flagForgeConfigFile); err == nil {
				linkDir = loaded.StateDir()
			}
		}
		if linkDir == "" {
			linkDir = ".azrbnkctl"
		}
		l, err := forge.ReadLink(linkDir)
		if err != nil || !l.IsRegistered() {
			return nil
		}
		return l
	}
)

var forgeScanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Index the cluster's BNK 2.4 resources, check readiness, discover MCP endpoints",
	Long: `forge scan reads the cluster directly and reports what a BNK 2.4 deployment
must show before Forge can manage it:

  API generation   gateway.k8s.f5.com served (2.4), gateway.k8s.f5net.com (2.3), or both
  Infra            Programmed=True
  Gateway          Accepted=True and Programmed=True
  controller       f5-cne-controller rollout complete
  policies         GatewaySettings, EgressGateway, SecPolicy, NetPolicy, F5BigPersistenceProfile

It then walks the HTTPRoutes for MCP tool servers (annotation
bnk.f5.com/protocol=mcp or a path containing /mcp) and prints each one with its
VIP URLs, hostnames, backends, governance iRules, SecPolicies and the
MODEL_CONTEXT_PROTOCOL persistence profile pinning its sessions.

--probe calls initialize + tools/list on every endpoint over Streamable HTTP
and records the tool catalogue. The VIP is private, so run this where the VIP
is reachable (the jumphost, or through a port-forward). --bearer-env names an
environment variable holding the bearer token the governance iRule expects.

--register-targets writes every endpoint into Forge's Target Catalog
(POST /api/benchmarks/targets) with the MCP metadata as tags; idempotent.
--remote also runs Forge's own scan_cluster + bnk_health over MCP and warns
when Forge indexed a 2.3 resource registry against a 2.4 cluster.

Kubeconfig: --kubeconfig, else the cluster.yaml state (--config/-f), else the
kubectl default. Forge identity: forge_link.json in the state dir.`,
	Args: cobra.NoArgs,
	RunE: runForgeScan,
}

func init() {
	f := forgeScanCmd.Flags()
	addScanFlags(f)
	f.BoolVar(&flagForgeScanRegister, "register-targets", false, "register every MCP endpoint in Forge's Target Catalog")
	f.BoolVar(&flagForgeScanRemote, "remote", false, "also run Forge's scan_cluster + bnk_health for the linked cluster")
	forgeCmd.AddCommand(forgeScanCmd)
}

// forgeScanOutput is the -o json document.
type forgeScanOutput struct {
	Cluster string            `json:"cluster,omitempty"`
	Index   *bnkscan.Index    `json:"index"`
	Remote  *forgeRemoteScan  `json:"forge,omitempty"`
	Targets []forgeScanTarget `json:"targets,omitempty"`
	Probes  map[string]string `json:"probeErrors,omitempty"`
}

type forgeRemoteScan struct {
	ClusterID  int                 `json:"clusterId"`
	Scan       *forge.ScanResult   `json:"scan,omitempty"`
	Health     *forge.HealthResult `json:"health,omitempty"`
	ScanErr    string              `json:"scanError,omitempty"`
	HealthErr  string              `json:"healthError,omitempty"`
	Warning    string              `json:"warning,omitempty"`
	MCPURL     string              `json:"mcpUrl"`
	Generation bnkscan.Generation  `json:"generation,omitempty"`
}

func runForgeScan(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, flagForgeScanTimeout)
	defer cancel()

	cl, idx, probes, err := discoverMCP(ctx, flagForgeConfigFile, flagForgeScanProbe)
	if err != nil {
		return fmt.Errorf("forge scan: %w", err)
	}
	out := forgeScanOutput{Index: idx, Probes: probes}
	if cl != nil {
		out.Cluster = cl.Metadata.Name
	}

	// Forge identity (optional): only needed for --remote and --register-targets.
	link := forgeScanLink(cl)

	if flagForgeScanRemote {
		out.Remote = remoteForgeScan(ctx, cl, link, idx.Generation)
	}
	if flagForgeScanRegister {
		if link == nil {
			return errors.New("forge scan --register-targets: the cluster is not registered with Forge (no forge_link.json); run `azrbnkctl forge register` first")
		}
		out.Targets = registerDiscoveredTargets(ctx, cl, link, "", forge.RestCreds{}, idx.MCP)
	}

	if flagOutput == "json" {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	w := cmd.OutOrStdout()
	if out.Cluster != "" {
		fmt.Fprintf(w, "cluster: %s\n", out.Cluster)
	}
	bnkscan.WriteReport(w, idx)
	for name, perr := range out.Probes {
		fmt.Fprintf(w, "probe %s: %s\n", name, perr)
	}
	if r := out.Remote; r != nil {
		fmt.Fprintf(w, "forge (%s, cluster_id=%d)\n", r.MCPURL, r.ClusterID)
		if r.Scan != nil {
			fmt.Fprintf(w, "  scan:    %s\n", r.Scan.Summary())
		} else if r.ScanErr != "" {
			fmt.Fprintf(w, "  scan:    %s\n", r.ScanErr)
		}
		if r.Health != nil {
			fmt.Fprintf(w, "  health:  %s\n", r.Health.Summary())
		} else if r.HealthErr != "" {
			fmt.Fprintf(w, "  health:  %s\n", r.HealthErr)
		}
		if r.Warning != "" {
			fmt.Fprintf(w, "  warning: %s\n", r.Warning)
		}
	}
	writeTargetResults(w, out.Targets)
	if !idx.Ready() {
		return errors.New("forge scan: cluster is not ready (see above)")
	}
	return nil
}

// probeEndpoints fills MCPEndpoint.Tools from a live tools/list and returns
// the per-endpoint errors.
func probeEndpoints(ctx context.Context, idx *bnkscan.Index) map[string]string {
	errs := map[string]string{}
	headers := map[string]string{}
	if flagForgeScanBearerEnv != "" {
		if tok := os.Getenv(flagForgeScanBearerEnv); tok != "" {
			headers["Authorization"] = "Bearer " + tok
		} else {
			errs["bearer"] = fmt.Sprintf("--bearer-env %s is empty", flagForgeScanBearerEnv)
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	for i := range idx.MCP {
		ep := &idx.MCP[i]
		url := ep.PrimaryURL()
		if url == "" {
			errs[ep.Name()] = "no URL (Gateway has no address or HTTP listener)"
			continue
		}
		h := map[string]string{}
		for k, v := range headers {
			h[k] = v
		}
		if len(ep.Hostnames) > 0 && !strings.Contains(ep.Hostnames[0], "*") {
			h["Host"] = ep.Hostnames[0]
		}
		res, err := forgeScanProbe(ctx, client, url, h)
		if err != nil {
			errs[ep.Name()] = err.Error()
			continue
		}
		ep.Tools = res.Tools
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// pickMCPURL returns the MCP URL to use, preferring explicit flag over intent.
func pickMCPURL(intentMCP string) string {
	if flagForgeMCPURL != "" {
		return flagForgeMCPURL
	}
	return intentMCP
}

// remoteForgeScan runs scan_cluster + bnk_health through forge and compares
// the API generation forge saw with the one read directly from the cluster.
func remoteForgeScan(ctx context.Context, cl *intent.Cluster, link *forge.Link, local bnkscan.Generation) *forgeRemoteScan {
	mcpURL := ""
	if cl != nil && cl.Forge != nil {
		mcpURL = cl.Forge.MCPURL
	}
	if link != nil && link.ForgeMCPURL != "" && pickMCPURL(mcpURL) == "" {
		mcpURL = link.ForgeMCPURL
	}
	fc := forge.NewClient(pickMCPURL(mcpURL))
	r := &forgeRemoteScan{MCPURL: fc.URL()}
	if link == nil {
		r.ScanErr = "cluster is not registered with Forge (no forge_link.json)"
		return r
	}
	r.ClusterID = link.ClusterID
	if scan, _, err := fc.ScanClusterTyped(ctx, link.ClusterID); err != nil {
		r.ScanErr = err.Error()
	} else {
		r.Scan = &scan
		r.Generation = scan.Generation()
		if (local == bnkscan.Gen24 || local == bnkscan.GenMixed) && !scan.Indexes24() {
			r.Warning = "Forge did not index gateway.k8s.f5.com: its resource registry predates BNK 2.4, so Infra / GatewaySettings / SecPolicy / NetPolicy are missing from the Fleet view; upgrade Forge"
		}
	}
	if health, _, err := fc.BNKHealthTyped(ctx, link.ClusterID); err != nil {
		r.HealthErr = err.Error()
	} else {
		r.Health = &health
	}
	return r
}

func resolveForgeScanRestURL(cl *intent.Cluster, link *forge.Link) string {
	if flagForgeScanRestURL != "" {
		return flagForgeScanRestURL
	}
	if cl != nil && cl.Forge != nil {
		return cl.Forge.ResolveURL()
	}
	if link != nil && link.ForgeURL != "" {
		return link.ForgeURL
	}
	if v := os.Getenv("AZRBNKCTL_FORGE_URL"); v != "" {
		return v
	}
	return intent.DefaultForgeRESTURL
}

func resolveForgeScanCreds(cl *intent.Cluster) forge.RestCreds {
	u, p := flagForgeScanUser, flagForgeScanPass
	if u == "" {
		u = os.Getenv("AZRBNKCTL_FORGE_USERNAME")
	}
	if p == "" {
		p = os.Getenv("AZRBNKCTL_FORGE_PASSWORD")
	}
	if cl != nil && cl.Forge != nil {
		if u == "" {
			u = cl.Forge.ResolveUsername()
		}
		if p == "" {
			p, _ = cl.Forge.ResolvePassword()
		}
	}
	return forge.RestCreds{Username: u, Password: p}
}

// forgeScanSummary renders scan_cluster output as one line: the typed summary
// (BNK status, API generation, versions) when it parses, the raw text otherwise.
func forgeScanSummary(raw string) string {
	if res, err := forge.ParseScanResult(raw); err == nil {
		s := res.Summary()
		if res.Generation() == "2.3" {
			s += "  (2.3 API: run azrbnkctl bnk upgrade before Forge can manage 2.4 resources)"
		}
		return s
	}
	return oneLine(raw)
}

// forgeHealthSummary renders bnk_health output as one line.
func forgeHealthSummary(raw string) string {
	if res, err := forge.ParseHealthResult(raw); err == nil && res.Overall != "" {
		return res.Summary()
	}
	return oneLine(raw)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
