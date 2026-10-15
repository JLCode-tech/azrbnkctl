package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/topology"
)

var (
	flagTopologyFile       string
	flagTopologyFormat     string
	flagTopologyLive       bool
	flagTopologyStatic     bool
	flagTopologyKubeconfig string
)

var topologyCmd = &cobra.Command{
	Use:   "topology",
	Short: "Render the VNet, subnet, AKS agent pool, TMM data-plane, and jumphost topology",
	Long: `topology combines the declarative cluster.yaml with any provisioned IDs in
.azrbnkctl/<workspace>/state.env (and, when reachable, live Kubernetes node and
pod counts) to render a single view of the cluster.

Supported output formats (--format or -o / --output):
  ascii    Human-readable box diagram (default)
  mermaid  Mermaid flowchart TB suitable for Markdown / GitHub / Notion
  dot      Graphviz DOT digraph (pipe to 'dot -Tsvg' or 'dot -Tpng')
  json     Machine-readable topology snapshot`,
	Example: `  # Default ASCII diagram (overlays state.env + live k8s if kubeconfig exists)
  azrbnkctl topology -f cluster.yaml

  # Purely declarative view from cluster.yaml + state.env (no k8s calls)
  azrbnkctl topology -f cluster.yaml --static

  # Emit a Mermaid diagram for README / docs
  azrbnkctl topology -f cluster.yaml --format mermaid

  # Pipe Graphviz DOT to SVG
  azrbnkctl topology -f cluster.yaml -o dot | dot -Tsvg > topology.svg`,
	RunE: runTopologyCmd,
}

func init() {
	topologyCmd.Flags().StringVarP(&flagTopologyFile, "config", "f", "cluster.yaml", "Path to cluster.yaml")
	topologyCmd.Flags().StringVar(&flagTopologyFormat, "format", "ascii", "Output format: ascii | mermaid | dot | json")
	topologyCmd.Flags().BoolVar(&flagTopologyLive, "live", true, "Query live Kubernetes node and pod counts when a kubeconfig is available")
	topologyCmd.Flags().BoolVar(&flagTopologyStatic, "static", false, "Skip live Kubernetes queries (declarative + state.env only)")
	topologyCmd.Flags().StringVar(&flagTopologyKubeconfig, "kubeconfig", "", "Explicit kubeconfig path (defaults to .azrbnkctl/<workspace>/kubeconfig)")
	rootCmd.AddCommand(topologyCmd)
}

func runTopologyCmd(cmd *cobra.Command, _ []string) error {
	cl, err := intent.Load(flagTopologyFile)
	if err != nil {
		return err
	}

	var st *state.State
	statePath := filepath.Join(cl.StateDir(), "state.env")
	if _, statErr := os.Stat(statePath); statErr == nil {
		if loaded, openErr := state.Load(cl.StateDir()); openErr == nil {
			st = loaded
		}
	}

	m := topology.Build(cl, st)

	if flagTopologyLive && !flagTopologyStatic {
		kc := resolveTopologyKubeconfig(cl, st)
		if kc != "" {
			if cs, kerr := k8s.BuildClientset(kc); kerr == nil {
				ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				if eerr := topology.EnrichFromK8s(ctx, cs, &m); eerr != nil && flagVerbose {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: live k8s enrichment incomplete: %v\n", eerr)
				}
				cancel()
			} else if flagVerbose {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: skipping live k8s enrichment (%v)\n", kerr)
			}
		}
	}

	format := flagTopologyFormat
	if !cmd.Flags().Changed("format") && flagOutput != "" && flagOutput != "human" {
		format = flagOutput
	}
	format = strings.ToLower(strings.TrimSpace(format))

	out := cmd.OutOrStdout()
	switch format {
	case "", "ascii", "text", "human":
		fmt.Fprint(out, topology.RenderASCII(m))
	case "mermaid", "mmd":
		fmt.Fprint(out, topology.RenderMermaid(m))
	case "dot", "graphviz":
		fmt.Fprint(out, topology.RenderDOT(m))
	case "json":
		s, jerr := topology.RenderJSON(m)
		if jerr != nil {
			return jerr
		}
		fmt.Fprint(out, s)
	default:
		return fmt.Errorf("unsupported --format %q (want ascii, mermaid, dot, or json)", format)
	}
	return nil
}

func resolveTopologyKubeconfig(cl *intent.Cluster, st *state.State) string {
	if flagTopologyKubeconfig != "" {
		if _, err := os.Stat(flagTopologyKubeconfig); err == nil {
			return flagTopologyKubeconfig
		}
		return ""
	}
	if st != nil {
		if kc := st.Get("KUBECONFIG_PATH"); kc != "" {
			if _, err := os.Stat(kc); err == nil {
				return kc
			}
		}
	}
	candidate := filepath.Join(cl.StateDir(), "kubeconfig")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
