package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

var validateCmd = &cobra.Command{
	Use:   "validate <path>",
	Short: "Parse and validate a cluster.yaml (no Azure API calls)",
	Long: `azrbnkctl validate reads the given cluster.yaml, applies strict
schema validation (unknown fields, subnet CIDR containment, VM sizing,
Kubernetes version >= 1.32, JWT path check) and exits non-zero on any failure.

It makes no Azure API calls — useful for CI checks and local validation loops.

Example:
  azrbnkctl validate examples/dual-interface/cluster.yaml`,
	Args: cobra.ExactArgs(1),
	RunE: runValidate,
}

func runValidate(_ *cobra.Command, args []string) error {
	path := args[0]
	c, err := intent.Load(path)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✓ %s parses cleanly\n", path)
	fmt.Fprintf(os.Stderr, "  apiVersion=%s kind=%s name=%s subscription=%s location=%s\n",
		c.APIVersion, c.Kind, c.Metadata.Name, c.Metadata.SubscriptionID, c.Metadata.Location)
	fmt.Fprintf(os.Stderr, "  network: vnet=%s(%s) system=%s ext=%s int=%s\n",
		c.Derived.VNetName, c.Network.VNetCIDR,
		c.Network.Subnets.System, c.Network.Subnets.External, c.Network.Subnets.Internal)
	fmt.Fprintf(os.Stderr, "  cluster: k8s=%s systemNodeSize=%s tmmNodeSize=%s\n",
		c.ClusterSpec.KubernetesVersion, c.ClusterSpec.SystemNodePool.VMSize, c.ClusterSpec.TMMNodePool.VMSize)
	fmt.Fprintf(os.Stderr, "  bnk: manifestVersion=%s tmmMtu=%d\n",
		c.Bnk.ManifestVersion, c.Bnk.TmmMtu)
	fmt.Fprintf(os.Stderr, "  stateDir=%s\n", c.StateDir())
	return nil
}

func init() {
	rootCmd.AddCommand(validateCmd)
}
