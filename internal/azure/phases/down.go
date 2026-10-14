package phases

import (
	"context"
	"fmt"
	"os"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

// PhaseDownCluster tears down the AKS managed cluster and waits for completion.
func PhaseDownCluster(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	rgName := cl.Derived.ResourceGroupName
	clusterName := cl.Derived.AKSClusterName

	fmt.Fprintf(os.Stderr, "[down] aks-cluster: deleting %s in %s\n", clusterName, rgName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete AKS cluster %s\n", clusterName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		fmt.Fprintln(os.Stderr, "[down] AZRBNKCTL_SKIP_AUTH=1: skipping real AKS deletion")
		return nil
	}

	poller, err := clients.ManagedClusters.BeginDelete(ctx, rgName, clusterName, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[down] AKS cluster %s delete call returned: %v (may already be gone)\n", clusterName, err)
		return nil
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[down] waiting for AKS delete: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "[down] AKS cluster %s deleted\n", clusterName)
	return nil
}

// PhaseDownNAT tears down the NAT Gateway and associated Public IP.
func PhaseDownNAT(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	rgName := cl.Derived.ResourceGroupName
	natName := cl.Derived.NatGatewayName
	pipName := cl.Derived.PublicIPName

	fmt.Fprintf(os.Stderr, "[down] nat-gateway: deleting %s and public-ip %s\n", natName, pipName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete NAT Gateway %s and Public IP %s\n", natName, pipName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		return nil
	}

	if poller, err := clients.NatGateways.BeginDelete(ctx, rgName, natName, nil); err == nil {
		_, _ = poller.PollUntilDone(ctx, nil)
	}

	if poller, err := clients.PublicIPAddresses.BeginDelete(ctx, rgName, pipName, nil); err == nil {
		_, _ = poller.PollUntilDone(ctx, nil)
	}

	return nil
}

// PhaseDownNetworks tears down the VNet, Subnets, and NSG.
func PhaseDownNetworks(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	rgName := cl.Derived.ResourceGroupName
	vnetName := cl.Derived.VNetName
	nsgName := fmt.Sprintf("nsg-%s", cl.Metadata.Name)

	fmt.Fprintf(os.Stderr, "[down] networks: deleting VNet %s and NSG %s\n", vnetName, nsgName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete VNet %s and NSG %s\n", vnetName, nsgName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		return nil
	}

	if poller, err := clients.VirtualNetworks.BeginDelete(ctx, rgName, vnetName, nil); err == nil {
		_, _ = poller.PollUntilDone(ctx, nil)
	}

	if poller, err := clients.SecurityGroups.BeginDelete(ctx, rgName, nsgName, nil); err == nil {
		_, _ = poller.PollUntilDone(ctx, nil)
	}

	return nil
}

// PhaseDownResourceGroup deletes the Resource Group only if deleteRG is explicitly true.
// By default, the Resource Group is preserved to avoid accidental destruction of shared scopes.
func PhaseDownResourceGroup(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, deleteRG, dryRun bool) error {
	rgName := cl.Derived.ResourceGroupName

	if !deleteRG {
		fmt.Fprintf(os.Stderr, "[down] resource-group: keeping %s (Resource Group is preserved by default; pass --delete-rg to delete)\n", rgName)
		return nil
	}

	fmt.Fprintf(os.Stderr, "[down] resource-group: deleting %s (--delete-rg specified)\n", rgName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete Resource Group %s\n", rgName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		return nil
	}

	if poller, err := clients.ResourceGroups.BeginDelete(ctx, rgName, nil); err == nil {
		_, _ = poller.PollUntilDone(ctx, nil)
	}

	fmt.Fprintf(os.Stderr, "[down] Resource Group %s deleted\n", rgName)
	return nil
}
