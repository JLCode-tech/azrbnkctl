package phases

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
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

	// Best-effort strip of F5 BNK CR finalizers before deleting the AKS cluster.
	if st != nil {
		if kcPath := st.KubeconfigPath(); kcPath != "" {
			if _, err := os.Stat(kcPath); err == nil {
				stripStuckFinalizers(ctx, kcPath)
			}
		}
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

// stripStuckFinalizers best-effort strips finalizers from F5 BNK CRs that
// otherwise block namespace and cluster deletion when controller pods are gone.
func stripStuckFinalizers(ctx context.Context, kubeconfigPath string) {
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(kubeconfigPath, "")
	if err != nil {
		return
	}
	dyn, err := k8s.NewDynamicFromConfig(cfg)
	if err != nil {
		return
	}

	gvrs := []schema.GroupVersionResource{
		{Group: "k8s.f5net.com", Version: "v1", Resource: "f5-spk-vlans"},
		{Group: "k8s.f5net.com", Version: "v1", Resource: "f5-spk-staticroutes"},
		{Group: "k8s.f5net.com", Version: "v1", Resource: "f5-spk-egresses"},
		{Group: "k8s.f5net.com", Version: "v1", Resource: "f5-spk-snatpools"},
		{Group: "k8s.f5.com", Version: "v1", Resource: "cneinstances"},
	}
	namespaces := []string{"f5-cne-core", "f5-cne-system", "f5-bnk-instance"}

	for _, ns := range namespaces {
		for _, gvr := range gvrs {
			list, listErr := dyn.Resource(gvr).Namespace(ns).List(tctx, metav1.ListOptions{})
			if listErr != nil || list == nil {
				continue
			}
			for i := range list.Items {
				item := list.Items[i]
				if len(item.GetFinalizers()) == 0 {
					continue
				}
				unstructured.RemoveNestedField(item.Object, "metadata", "finalizers")
				if _, updErr := dyn.Resource(gvr).Namespace(ns).Update(tctx, &item, metav1.UpdateOptions{}); updErr == nil {
					fmt.Fprintf(os.Stderr, "[down] stripped finalizers from %s %s/%s\n", gvr.Resource, ns, item.GetName())
				}
			}
		}
	}
}
