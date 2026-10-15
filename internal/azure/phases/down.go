package phases

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
)

func isAzureNotFound(err error) bool {
	if err == nil {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		if respErr.StatusCode == http.StatusNotFound {
			return true
		}
		switch respErr.ErrorCode {
		case "ResourceNotFound", "ResourceGroupNotFound", "ParentResourceNotFound", "NotFound":
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "resourcenotfound") ||
		strings.Contains(msg, "resourcegroupnotfound") ||
		strings.Contains(msg, "status code: 404") ||
		strings.Contains(msg, "http 404")
}

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
		if isAzureNotFound(err) {
			fmt.Fprintf(os.Stderr, "[down] AKS cluster %s already absent\n", clusterName)
			return nil
		}
		return fmt.Errorf("delete AKS cluster %s in %s: %w", clusterName, rgName, err)
	}

	_, err = poller.PollUntilDone(ctx, nil)
	if err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("waiting for AKS cluster %s delete: %w", clusterName, err)
	}

	fmt.Fprintf(os.Stderr, "[down] AKS cluster %s deleted\n", clusterName)
	return nil
}

// PhaseDownNAT tears down the NAT Gateway and associated Public IPs.
func PhaseDownNAT(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	rgName := cl.Derived.ResourceGroupName
	natName := cl.Derived.NatGatewayName
	natPipName := cl.Derived.NatGatewayPublicIPName
	pipName := cl.Derived.PublicIPName

	fmt.Fprintf(os.Stderr, "[down] nat-gateway: deleting %s and public-ips (%s, %s)\n", natName, natPipName, pipName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete NAT Gateway %s and Public IPs (%s, %s)\n", natName, natPipName, pipName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		return nil
	}

	if poller, err := clients.NatGateways.BeginDelete(ctx, rgName, natName, nil); err != nil {
		if !isAzureNotFound(err) {
			return fmt.Errorf("delete NAT Gateway %s in %s: %w", natName, rgName, err)
		}
	} else if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("waiting for NAT Gateway %s delete: %w", natName, err)
	}

	if natPipName != "" {
		if poller, err := clients.PublicIPAddresses.BeginDelete(ctx, rgName, natPipName, nil); err != nil {
			if !isAzureNotFound(err) {
				return fmt.Errorf("delete NAT Public IP %s in %s: %w", natPipName, rgName, err)
			}
		} else if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
			return fmt.Errorf("waiting for NAT Public IP %s delete: %w", natPipName, err)
		}
	}

	if pipName != "" && pipName != natPipName {
		if poller, err := clients.PublicIPAddresses.BeginDelete(ctx, rgName, pipName, nil); err != nil {
			if !isAzureNotFound(err) {
				return fmt.Errorf("delete Public IP %s in %s: %w", pipName, rgName, err)
			}
		} else if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
			return fmt.Errorf("waiting for Public IP %s delete: %w", pipName, err)
		}
	}

	return nil
}

// PhaseDownNetworks tears down the VNet, Subnets, and NSG.
func PhaseDownNetworks(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	rgName := cl.Derived.ResourceGroupName
	vnetName := cl.Derived.VNetName
	nsgName := cl.Derived.NSGName
	if nsgName == "" {
		nsgName = fmt.Sprintf("nsg-%s", cl.Metadata.Name)
	}

	fmt.Fprintf(os.Stderr, "[down] networks: deleting VNet %s and NSG %s\n", vnetName, nsgName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete VNet %s and NSG %s\n", vnetName, nsgName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		return nil
	}

	if poller, err := clients.VirtualNetworks.BeginDelete(ctx, rgName, vnetName, nil); err != nil {
		if !isAzureNotFound(err) {
			return fmt.Errorf("delete VNet %s in %s: %w", vnetName, rgName, err)
		}
	} else if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("waiting for VNet %s delete: %w", vnetName, err)
	}

	if poller, err := clients.SecurityGroups.BeginDelete(ctx, rgName, nsgName, nil); err != nil {
		if !isAzureNotFound(err) {
			return fmt.Errorf("delete NSG %s in %s: %w", nsgName, rgName, err)
		}
	} else if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("waiting for NSG %s delete: %w", nsgName, err)
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

	if poller, err := clients.ResourceGroups.BeginDelete(ctx, rgName, nil); err != nil {
		if !isAzureNotFound(err) {
			return fmt.Errorf("delete Resource Group %s: %w", rgName, err)
		}
	} else if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("waiting for Resource Group %s delete: %w", rgName, err)
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

	crds := []string{
		"f5-spk-vlans.k8s.f5net.com",
		"f5-spk-staticroutes.k8s.f5net.com",
		"f5-spk-egresses.k8s.f5net.com",
		"f5-spk-snatpools.k8s.f5net.com",
		"infras.gateway.k8s.f5.com",
		"gatewaysettings.gateway.k8s.f5.com",
		"egressgateways.gateway.k8s.f5.com",
		"analyzers.k8s.f5.com",
		"cnecontrollers.k8s.f5.com",
		"cneinstances.k8s.f5.com",
		"dssms.k8s.f5.com",
		"f5tmms.k8s.f5.com",
		"coremonds.k8s.f5.com",
		"crdconversions.k8s.f5.com",
		"crdinstallers.k8s.f5.com",
		"csrcs.k8s.f5.com",
		"cwcs.k8s.f5.com",
		"ipamcontrollers.k8s.f5.com",
		"rabbitmqs.k8s.f5.com",
		"ipams.fic.f5.com",
		"ipamranges.fic.f5.com",
	}
	gvrs := crdsToGVRs(crds)
	namespaces := []string{
		"f5-cne-core",
		"f5-cne-system",
		"f5-bnk-instance",
		"f5-utils",
		"f5-bnk",
		"bnk-demo",
		"egress-demo",
	}

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

func crdsToGVRs(crds []string) []schema.GroupVersionResource {
	var gvrs []schema.GroupVersionResource
	for _, crd := range crds {
		var resource, group string
		for i := 0; i < len(crd); i++ {
			if crd[i] == '.' {
				resource = crd[:i]
				group = crd[i+1:]
				break
			}
		}
		if resource == "" || group == "" {
			continue
		}
		version := "v1"
		if group == "gateway.k8s.f5.com" {
			version = "v1alpha1"
		}
		gvrs = append(gvrs, schema.GroupVersionResource{Group: group, Version: version, Resource: resource})
		if resource == "f5-spk-egresses" {
			gvrs = append(gvrs, schema.GroupVersionResource{Group: group, Version: "v3", Resource: resource})
		}
	}
	return gvrs
}
