package phases

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

// Phase23Kubeconfig retrieves the admin kubeconfig from AKS and writes it locally.
func Phase23Kubeconfig(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	rgName := cl.Derived.ResourceGroupName
	clusterName := cl.Derived.AKSClusterName

	fmt.Fprintf(os.Stderr, "[phase 23] kubeconfig: retrieving for cluster %s\n", clusterName)

	kubeconfigPath := st.KubeconfigPath()
	if dryRun {
		st.Set("KUBECONFIG_PATH", kubeconfigPath)
		fmt.Fprintf(os.Stderr, "[phase 23] [dry-run] would save kubeconfig to %s\n", kubeconfigPath)
		return nil
	}

	resp, err := clients.ManagedClusters.ListClusterAdminCredentials(ctx, rgName, clusterName, nil)
	if err != nil {
		return fmt.Errorf("phase23: ListClusterAdminCredentials %s: %w", clusterName, err)
	}

	if len(resp.Kubeconfigs) == 0 || resp.Kubeconfigs[0].Value == nil {
		return fmt.Errorf("phase23: no kubeconfig returned for cluster %s", clusterName)
	}

	kubeconfigData := resp.Kubeconfigs[0].Value

	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0o700); err != nil {
		return fmt.Errorf("phase23: creating kubeconfig directory: %w", err)
	}

	if err := os.WriteFile(kubeconfigPath, kubeconfigData, 0o600); err != nil {
		return fmt.Errorf("phase23: writing kubeconfig to %s: %w", kubeconfigPath, err)
	}

	st.Set("KUBECONFIG_PATH", kubeconfigPath)
	if err := st.Save(); err != nil {
		return fmt.Errorf("phase23: saving state: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[phase 23] kubeconfig saved to %s\n", kubeconfigPath)
	return nil
}
