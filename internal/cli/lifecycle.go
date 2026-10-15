package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/phases"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

var (
	flagConfig     string
	flagUpDryRun   bool
	flagDownDryRun bool
	flagYes        bool
	flagKeepRG     bool
	flagDeleteRG   bool
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Provision the AKS cluster + BNK stack (requires -f <cluster.yaml>)",
	Long: `azrbnkctl up drives the full provisioning graph: Azure Resource Group,
NSG, VNet and subnets, NAT Gateway, Public IPs, AKS Managed Cluster (system pool),
TMM Node Pool with Accelerated Networking, Kubeconfig retrieval, Node labels,
Interface discovery, namespaces, cert-manager, FLO, certs, CNEInstance, License,
Gateway API, VLANs and Routes, Activation verification, and Egress.

  azrbnkctl up -f cluster.yaml [--dry-run]`,
	RunE: runUp,
}

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Destroy the AKS cluster and managed resources (requires -f <cluster.yaml>)",
	Long: `azrbnkctl down deletes the AKS cluster, NAT Gateway, Public IPs,
VNet, Subnets, and NSG in reverse dependency order.
The Azure Resource Group is preserved by default to safeguard shared scopes and RBAC.
To also delete the Resource Group, pass --delete-rg.

  azrbnkctl down -f cluster.yaml [--yes] [--delete-rg] [--dry-run]`,
	RunE: runDown,
}

func init() {
	upCmd.Flags().StringVarP(&flagConfig, "config", "f", "", "path to cluster.yaml (required)")
	upCmd.Flags().BoolVar(&flagUpDryRun, "dry-run", false, "print the phased plan and exit 0 with no Azure mutations")

	downCmd.Flags().StringVarP(&flagConfig, "config", "f", "", "path to cluster.yaml (required)")
	downCmd.Flags().BoolVar(&flagDownDryRun, "dry-run", false, "print the destroy plan and exit 0 with no Azure mutations")
	downCmd.Flags().BoolVar(&flagYes, "yes", false, "skip the interactive destroy confirmation")
	downCmd.Flags().BoolVar(&flagDeleteRG, "delete-rg", false, "also delete the Azure Resource Group (CAUTION: destroys the entire Resource Group; defaults to false)")
	downCmd.Flags().BoolVar(&flagKeepRG, "keep-rg", false, "keep the Azure Resource Group (default behavior, preserved for backward compatibility)")

	rootCmd.AddCommand(upCmd, downCmd)
}

func runUp(cmd *cobra.Command, _ []string) error {
	if flagConfig == "" {
		return errors.New("azrbnkctl up requires -f <cluster.yaml>")
	}
	return runPhasedUp(cmd.Context(), flagConfig, flagUpDryRun)
}

func runDown(cmd *cobra.Command, _ []string) error {
	if flagConfig == "" {
		return errors.New("azrbnkctl down requires -f <cluster.yaml>")
	}
	if flagDeleteRG && flagKeepRG {
		return errors.New("cannot specify both --delete-rg and --keep-rg")
	}
	return runPhasedDown(cmd.Context(), flagConfig, flagYes, flagDeleteRG, flagDownDryRun)
}

type upPhase struct {
	num  string
	name string
	fn   func(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error
}

var upPhases = []upPhase{
	{num: "00", name: "preflight", fn: phases.Phase00Preflight},
	{num: "10", name: "resource-group", fn: phases.Phase10ResourceGroup},
	{num: "11", name: "nsg", fn: phases.Phase11NSG},
	{num: "12", name: "vnet", fn: phases.Phase12VNet},
	{num: "13", name: "nat-gateway", fn: phases.Phase13NatGateway},
	{num: "14", name: "public-ips", fn: phases.Phase14PublicIPs},
	{num: "21", name: "aks-cluster", fn: phases.Phase21AKSCluster},
	{num: "22", name: "tmm-nodepool", fn: phases.Phase22TMMNodePool},
	{num: "23", name: "kubeconfig", fn: phases.Phase23Kubeconfig},
	{num: "24", name: "node-labels", fn: phases.Phase24NodeLabels},
	{num: "25", name: "discovery", fn: phases.Phase25Discovery},
	{num: "29", name: "forge-register", fn: phases.Phase29ForgeRegister},
	{num: "30", name: "namespaces", fn: phases.Phase30Namespaces},
	{num: "31", name: "cert-manager", fn: phases.Phase31CertManager},
	{num: "31b", name: "metrics-server", fn: phases.Phase31bMetricsServer},
	{num: "33", name: "cwc-certs", fn: phases.Phase33CWCCerts},
	{num: "32", name: "flo", fn: phases.Phase32FLO},
	{num: "34", name: "cneinstance", fn: phases.Phase34CNEInstance},
	{num: "35", name: "license", fn: phases.Phase35License},
	{num: "36", name: "gateway-api", fn: phases.Phase36GatewayAPI},
	{num: "37", name: "vlans-routes", fn: phases.Phase37VlansRoutes},
	{num: "38", name: "cwc-heal", fn: phases.Phase38CWCHeal},
	{num: "38b", name: "dssm-overlay", fn: phases.Phase38bDSSMInsecureOverlay},
	{num: "38c", name: "pod-manager-heal", fn: phases.Phase38cPodManagerHeal},
	{num: "38d", name: "tmm-log-stream", fn: phases.Phase38dTMMLogStream},
	{num: "40", name: "activation", fn: phases.Phase40Activation},
	{num: "50", name: "egress", fn: phases.Phase50Egress},
}

func runPhasedUp(ctx context.Context, configPath string, dryRun bool) error {
	cl, err := intent.Load(configPath)
	if err != nil {
		return fmt.Errorf("up: %w", err)
	}

	st, err := state.Load(cl.StateDir())
	if err != nil {
		return fmt.Errorf("up: loading state: %w", err)
	}
	if dryRun {
		st.MarkReadOnly()
		fmt.Fprintln(os.Stderr, "→ dry-run: printing plan, no Azure mutations will be made")
	}

	clients, err := buildAzureClients(ctx, cl, dryRun)
	if err != nil {
		return fmt.Errorf("up: %w", err)
	}

	for _, p := range upPhases {
		if err := p.fn(ctx, cl, st, clients, dryRun); err != nil {
			return fmt.Errorf("up: phase %s (%s): %w", p.num, p.name, err)
		}
		if !dryRun {
			now := time.Now().UTC().Format(time.RFC3339)
			st.Set(fmt.Sprintf("PHASE_%s_APPLIED_AT", strings.ToUpper(p.num)), now)
			switch p.num {
			case "35":
				st.Set("LICENSE_APPLIED_AT", now)
			case "40":
				st.Set("CNEINSTANCE_READY_AT", now)
			}
			if err := st.Save(); err != nil {
				return fmt.Errorf("up: saving state after phase %s: %w", p.num, err)
			}
		}
	}

	if !dryRun {
		if err := st.Save(); err != nil {
			return fmt.Errorf("up: saving state: %w", err)
		}
	}

	if dryRun {
		fmt.Fprintln(os.Stderr, "→ dry-run complete")
		return nil
	}
	fmt.Fprintf(os.Stderr, "✓ up complete: cluster=%s state=%s\n", cl.Metadata.Name, cl.StateDir())
	return nil
}

func buildAzureClients(ctx context.Context, cl *intent.Cluster, dryRun bool) (*azure.Clients, error) {
	subID, err := azure.ResolveSubscriptionID(cl.Metadata.SubscriptionID)
	if err != nil {
		if dryRun && azure.ShouldSkipAuth() {
			return &azure.Clients{SubscriptionID: "00000000-0000-0000-0000-000000000000"}, nil
		}
		return nil, fmt.Errorf("resolving subscription ID: %w", err)
	}
	if azure.ShouldSkipAuth() {
		if !dryRun {
			return nil, errors.New("AZRBNKCTL_SKIP_AUTH=1 is only valid together with --dry-run")
		}
		return &azure.Clients{SubscriptionID: subID}, nil
	}
	return azure.NewClientsWithDefault(subID)
}

func confirmDestroy(in io.Reader, out io.Writer, name, rg, subID string, deleteRG bool) bool {
	if deleteRG {
		fmt.Fprintf(out, "WARNING: About to DESTROY cluster %q and DELETE RESOURCE GROUP %q (subscription=%s)!\n", name, rg, subID)
	} else {
		fmt.Fprintf(out, "About to DESTROY cluster %q (rg=%s [preserved], subscription=%s) and managed resources.\n", name, rg, subID)
	}
	fmt.Fprintln(out, "Type 'destroy' to proceed:")
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() || scanner.Text() != "destroy" {
		fmt.Fprintln(out, "Aborted.")
		return false
	}
	return true
}

func runPhasedDown(ctx context.Context, configPath string, yes, deleteRG, dryRun bool) error {
	cl, err := intent.Load(configPath)
	if err != nil {
		return fmt.Errorf("down: %w", err)
	}

	st, err := state.Load(cl.StateDir())
	if err != nil {
		return fmt.Errorf("down: loading state: %w", err)
	}

	if dryRun {
		printDownPlan(os.Stderr, cl, deleteRG)
		return nil
	}

	if !yes && !confirmDestroy(os.Stdin, os.Stderr, cl.Metadata.Name, cl.Derived.ResourceGroupName, cl.Metadata.SubscriptionID, deleteRG) {
		return nil
	}

	// Unregister from Forge before cloud resource destruction
	_ = phases.Phase29ForgeRegisterDown(ctx, cl, st, dryRun)

	clients, err := buildAzureClients(ctx, cl, dryRun)
	if err != nil {
		return fmt.Errorf("down: %w", err)
	}

	if err := phases.PhaseDownCluster(ctx, cl, st, clients, dryRun); err != nil {
		return fmt.Errorf("down: %w", err)
	}
	if err := phases.PhaseDownNAT(ctx, cl, st, clients, dryRun); err != nil {
		return fmt.Errorf("down: %w", err)
	}
	if err := phases.PhaseDownNetworks(ctx, cl, st, clients, dryRun); err != nil {
		return fmt.Errorf("down: %w", err)
	}
	if err := phases.PhaseDownResourceGroup(ctx, cl, st, clients, deleteRG, dryRun); err != nil {
		return fmt.Errorf("down: %w", err)
	}

	if err := st.Save(); err != nil {
		return fmt.Errorf("down: saving state: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✓ down complete: cluster=%s\n", cl.Metadata.Name)
	return nil
}

func printDownPlan(w io.Writer, cl *intent.Cluster, deleteRG bool) {
	fmt.Fprintf(w, "→ dry-run: destroy plan for cluster %q (rg=%s subscription=%s) — no Azure mutations\n",
		cl.Metadata.Name, cl.Derived.ResourceGroupName, cl.Metadata.SubscriptionID)

	var plan []string
	if cl.Forge != nil && cl.Forge.Enabled {
		plan = append(plan, fmt.Sprintf("  • unregister BNK Forge           %s", cl.Derived.AKSClusterName))
	}
	plan = append(plan, fmt.Sprintf("  • delete  AKS cluster                %s", cl.Derived.AKSClusterName))
	plan = append(plan, fmt.Sprintf("  • delete  NAT Gateway                %s", cl.Derived.NatGatewayName))
	plan = append(plan, fmt.Sprintf("  • delete  NAT Gateway Public IP      %s", cl.Derived.NatGatewayPublicIPName))
	plan = append(plan, fmt.Sprintf("  • delete  Ingress Public IP          %s", cl.Derived.PublicIPName))
	plan = append(plan, fmt.Sprintf("  • delete  VNet & Subnets             %s", cl.Derived.VNetName))
	plan = append(plan, fmt.Sprintf("  • delete  NSG                        %s", cl.Derived.NSGName))
	if deleteRG {
		plan = append(plan, fmt.Sprintf("  • delete  Resource Group             %s (--delete-rg)", cl.Derived.ResourceGroupName))
	} else {
		plan = append(plan, fmt.Sprintf("  • keep    Resource Group             %s (default: preserved)", cl.Derived.ResourceGroupName))
	}

	for _, line := range plan {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "→ dry-run complete: %d resource(s) planned, 0 Azure mutations made\n", len(plan))
}
