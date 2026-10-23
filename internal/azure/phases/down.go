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

func resolveAzureDownTarget(cl *intent.Cluster, st *state.State) (rg, aksClusterName, baseName string) {
	if st != nil {
		rg = strings.TrimSpace(st.Get("AZURE_RESOURCE_GROUP"))
		if rg == "" {
			rg = strings.TrimSpace(st.Get("RESOURCE_GROUP"))
		}
		aksClusterName = strings.TrimSpace(st.Get("AZURE_AKS_CLUSTER_NAME"))
		if aksClusterName == "" {
			aksClusterName = strings.TrimSpace(st.Get("CLUSTER_NAME"))
		}
	}
	if rg == "" && cl != nil {
		rg = strings.TrimSpace(cl.Metadata.ResourceGroup)
		if rg == "" {
			rg = strings.TrimSpace(cl.Derived.ResourceGroupName)
		}
	}
	if aksClusterName == "" && cl != nil {
		aksClusterName = strings.TrimSpace(cl.Derived.AKSClusterName)
		if aksClusterName == "" {
			aksClusterName = strings.TrimSpace(cl.Metadata.Name)
		}
	}
	if aksClusterName != "" {
		baseName = strings.TrimPrefix(aksClusterName, "aks-")
	} else if cl != nil {
		baseName = strings.TrimSpace(cl.Metadata.Name)
	}
	return rg, aksClusterName, baseName
}

func resolveAzureDownRGCandidates(cl *intent.Cluster, st *state.State) []string {
	rg, _, baseName := resolveAzureDownTarget(cl, st)
	if st != nil && (strings.TrimSpace(st.Get("AZURE_RESOURCE_GROUP")) != "" || strings.TrimSpace(st.Get("RESOURCE_GROUP")) != "") {
		return uniqueNonEmpty(rg)
	}
	if cl != nil && strings.TrimSpace(cl.Metadata.ResourceGroup) != "" {
		return uniqueNonEmpty(cl.Metadata.ResourceGroup)
	}
	if baseName != "" {
		return uniqueNonEmpty(rg, fmt.Sprintf("azrbnkctl-%s-rg", baseName), fmt.Sprintf("%s-rg", baseName))
	}
	return uniqueNonEmpty(rg)
}

func uniqueNonEmpty(items ...string) []string {
	var out []string
	for _, raw := range items {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		dup := false
		for _, existing := range out {
			if existing == s {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, s)
		}
	}
	return out
}

// PhaseDownCluster tears down the AKS TMM nodepool and managed cluster in reverse order.
func PhaseDownCluster(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	rgName, clusterName, baseName := resolveAzureDownTarget(cl, st)

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

	tmmPoolName := ""
	stateClusterName := ""
	if st != nil {
		stateClusterName = strings.TrimSpace(st.Get("AZURE_AKS_CLUSTER_NAME"))
		if stateClusterName == "" {
			stateClusterName = strings.TrimSpace(st.Get("CLUSTER_NAME"))
		}
		tmmPoolName = strings.TrimSpace(st.Get("AZURE_TMM_POOL_NAME"))
		if tmmPoolName == "" {
			tmmPoolName = strings.TrimSpace(st.Get("TMM_NODEPOOL_NAME"))
		}
	}
	if tmmPoolName == "" && cl != nil && strings.TrimSpace(cl.ClusterSpec.TMMNodePool.Name) != "" {
		tmmPoolName = strings.TrimSpace(cl.ClusterSpec.TMMNodePool.Name)
	}
	if tmmPoolName == "" {
		tmmPoolName = "tmm"
	}

	var clusterCandidates []string
	if stateClusterName != "" {
		clusterCandidates = uniqueNonEmpty(stateClusterName)
	} else if baseName != "" {
		clusterCandidates = uniqueNonEmpty(clusterName, baseName, "aks-"+baseName)
	} else {
		clusterCandidates = uniqueNonEmpty(clusterName)
	}

	rgCandidates := resolveAzureDownRGCandidates(cl, st)

	if clients.AgentPools != nil {
		for _, rg := range rgCandidates {
			for _, cand := range clusterCandidates {
				poller, err := clients.AgentPools.BeginDelete(ctx, rg, cand, tmmPoolName, nil)
				if err != nil {
					if !isAzureNotFound(err) {
						return fmt.Errorf("delete TMM nodepool %s in %s/%s: %w", tmmPoolName, rg, cand, err)
					}
				} else if poller != nil {
					if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
						return fmt.Errorf("waiting for TMM nodepool %s delete: %w", tmmPoolName, err)
					}
				}
			}
		}
	}

	if clients.ManagedClusters != nil {
		for _, rg := range rgCandidates {
			for _, cand := range clusterCandidates {
				poller, err := clients.ManagedClusters.BeginDelete(ctx, rg, cand, nil)
				if err != nil {
					if !isAzureNotFound(err) {
						return fmt.Errorf("delete AKS cluster %s in %s: %w", cand, rg, err)
					}
				} else if poller != nil {
					if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
						return fmt.Errorf("waiting for AKS cluster %s delete: %w", cand, err)
					}
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "[down] AKS cluster %s deleted\n", clusterName)
	return nil
}

// PhaseDownNAT tears down the NAT Gateway and associated Public IPs.
func PhaseDownNAT(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	_, _, baseName := resolveAzureDownTarget(cl, st)
	natName := ""
	natPipName := ""
	pipName := ""
	if cl != nil {
		natName = cl.Derived.NatGatewayName
		natPipName = cl.Derived.NatGatewayPublicIPName
		pipName = cl.Derived.PublicIPName
	}

	fmt.Fprintf(os.Stderr, "[down] nat-gateway: deleting %s and public-ips (%s, %s)\n", natName, natPipName, pipName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete NAT Gateway %s and Public IPs (%s, %s)\n", natName, natPipName, pipName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		return nil
	}

	stateNatName := ""
	stateNatPip := ""
	stateIngressPip := ""
	if st != nil {
		stateNatName = strings.TrimSpace(st.Get("AZURE_NAT_GW_NAME"))
		stateNatPip = strings.TrimSpace(st.Get("AZURE_NAT_PIP_NAME"))
		stateIngressPip = strings.TrimSpace(st.Get("AZURE_INGRESS_PIP_NAME"))
	}

	var natCandidates []string
	if stateNatName != "" {
		natCandidates = uniqueNonEmpty(stateNatName)
	} else if baseName != "" {
		natCandidates = uniqueNonEmpty(natName, fmt.Sprintf("%s-nat", baseName), fmt.Sprintf("natgw-%s", baseName))
	} else {
		natCandidates = uniqueNonEmpty(natName)
	}

	var pips []string
	if baseName != "" {
		pips = uniqueNonEmpty(
			stateNatPip,
			natPipName,
			fmt.Sprintf("%s-nat-pip", baseName),
			fmt.Sprintf("pip-nat-%s", baseName),
			stateIngressPip,
			pipName,
			fmt.Sprintf("pip-ingress-%s", baseName),
			fmt.Sprintf("%s-vip-pip", baseName),
		)
	} else {
		pips = uniqueNonEmpty(stateNatPip, natPipName, stateIngressPip, pipName)
	}

	rgCandidates := resolveAzureDownRGCandidates(cl, st)

	if clients.NatGateways != nil {
		for _, rg := range rgCandidates {
			for _, cand := range natCandidates {
				if poller, err := clients.NatGateways.BeginDelete(ctx, rg, cand, nil); err != nil {
					if !isAzureNotFound(err) {
						return fmt.Errorf("delete NAT Gateway %s in %s: %w", cand, rg, err)
					}
				} else if poller != nil {
					if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
						return fmt.Errorf("waiting for NAT Gateway %s delete: %w", cand, err)
					}
				}
			}
		}
	}

	if clients.PublicIPAddresses != nil {
		for _, rg := range rgCandidates {
			for _, pip := range pips {
				if poller, err := clients.PublicIPAddresses.BeginDelete(ctx, rg, pip, nil); err != nil {
					if !isAzureNotFound(err) {
						return fmt.Errorf("delete Public IP %s in %s: %w", pip, rg, err)
					}
				} else if poller != nil {
					if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
						return fmt.Errorf("waiting for Public IP %s delete: %w", pip, err)
					}
				}
			}
		}
	}

	return nil
}

// PhaseDownNetworks tears down the VNet, Subnets, and NSG.
func PhaseDownNetworks(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, dryRun bool) error {
	_, _, baseName := resolveAzureDownTarget(cl, st)
	vnetName := ""
	nsgName := ""
	if cl != nil {
		vnetName = cl.Derived.VNetName
		nsgName = cl.Derived.NSGName
		if nsgName == "" && cl.Metadata.Name != "" {
			nsgName = fmt.Sprintf("nsg-%s", cl.Metadata.Name)
		}
	}

	fmt.Fprintf(os.Stderr, "[down] networks: deleting VNet %s and NSG %s\n", vnetName, nsgName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[down] [dry-run] would delete VNet %s and NSG %s\n", vnetName, nsgName)
		return nil
	}

	if clients == nil || azure.ShouldSkipAuth() {
		return nil
	}

	stateVNet := ""
	stateNSG := ""
	if st != nil {
		stateVNet = strings.TrimSpace(st.Get("AZURE_VNET_NAME"))
		stateNSG = strings.TrimSpace(st.Get("AZURE_NSG_NAME"))
	}

	var vnetCandidates []string
	if stateVNet != "" {
		vnetCandidates = uniqueNonEmpty(stateVNet)
	} else if baseName != "" {
		vnetCandidates = uniqueNonEmpty(vnetName, fmt.Sprintf("%s-vnet", baseName), fmt.Sprintf("vnet-%s", baseName))
	} else {
		vnetCandidates = uniqueNonEmpty(vnetName)
	}

	var nsgCandidates []string
	if stateNSG != "" {
		nsgCandidates = uniqueNonEmpty(stateNSG)
	} else if baseName != "" {
		nsgCandidates = uniqueNonEmpty(nsgName, fmt.Sprintf("%s-nsg", baseName), fmt.Sprintf("nsg-%s", baseName))
	} else {
		nsgCandidates = uniqueNonEmpty(nsgName)
	}

	rgCandidates := resolveAzureDownRGCandidates(cl, st)

	if clients.VirtualNetworks != nil {
		for _, rg := range rgCandidates {
			for _, cand := range vnetCandidates {
				if poller, err := clients.VirtualNetworks.BeginDelete(ctx, rg, cand, nil); err != nil {
					if !isAzureNotFound(err) {
						return fmt.Errorf("delete VNet %s in %s: %w", cand, rg, err)
					}
				} else if poller != nil {
					if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
						return fmt.Errorf("waiting for VNet %s delete: %w", cand, err)
					}
				}
			}
		}
	}

	if clients.SecurityGroups != nil {
		for _, rg := range rgCandidates {
			for _, cand := range nsgCandidates {
				if poller, err := clients.SecurityGroups.BeginDelete(ctx, rg, cand, nil); err != nil {
					if !isAzureNotFound(err) {
						return fmt.Errorf("delete NSG %s in %s: %w", cand, rg, err)
					}
				} else if poller != nil {
					if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
						return fmt.Errorf("waiting for NSG %s delete: %w", cand, err)
					}
				}
			}
		}
	}

	return nil
}

// PhaseDownResourceGroup deletes the Resource Group only if deleteRG is explicitly true.
// By default, the Resource Group is preserved to avoid accidental destruction of shared scopes.
func PhaseDownResourceGroup(ctx context.Context, cl *intent.Cluster, st *state.State, clients *azure.Clients, deleteRG, dryRun bool) error {
	rgName, _, _ := resolveAzureDownTarget(cl, st)

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

	rgCandidates := resolveAzureDownRGCandidates(cl, st)
	if clients.ResourceGroups != nil {
		for _, rg := range rgCandidates {
			if poller, err := clients.ResourceGroups.BeginDelete(ctx, rg, nil); err != nil {
				if !isAzureNotFound(err) {
					return fmt.Errorf("delete Resource Group %s: %w", rg, err)
				}
			} else if poller != nil {
				if _, err := poller.PollUntilDone(ctx, nil); err != nil && !isAzureNotFound(err) {
					return fmt.Errorf("waiting for Resource Group %s delete: %w", rg, err)
				}
			}
		}
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
		"f5-big-dns-caches.k8s.f5net.com",
		"f5-big-persistence-profiles.k8s.f5net.com",
		"infras.gateway.k8s.f5.com",
		"gatewaysettings.gateway.k8s.f5.com",
		"egressgateways.gateway.k8s.f5.com",
		"l4routes.gateway.k8s.f5.com",
		"l4globalpolicies.gateway.k8s.f5.com",
		"apppolicies.gateway.k8s.f5.com",
		"airoutepolicies.inference.k8s.f5.com",
		"airoutes.inference.k8s.f5.com",
		"aigateways.inference.k8s.f5.com",
		"aigatewayprofiles.inference.k8s.f5.com",
		"aimodels.inference.k8s.f5.com",
		"aiproviders.inference.k8s.f5.com",
		"aicredentials.inference.k8s.f5.com",
		"aitenants.inference.k8s.f5.com",
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

	tenantGVR := schema.GroupVersionResource{Group: "inference.k8s.f5.com", Version: "v1alpha1", Resource: "aitenants"}
	if list, err := dyn.Resource(tenantGVR).List(tctx, metav1.ListOptions{}); err == nil && list != nil {
		for i := range list.Items {
			item := list.Items[i]
			if len(item.GetFinalizers()) == 0 {
				continue
			}
			unstructured.RemoveNestedField(item.Object, "metadata", "finalizers")
			_, _ = dyn.Resource(tenantGVR).Update(tctx, &item, metav1.UpdateOptions{})
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
		if group == "gateway.k8s.f5.com" || group == "inference.k8s.f5.com" {
			version = "v1alpha1"
		}
		gvrs = append(gvrs, schema.GroupVersionResource{Group: group, Version: version, Resource: resource})
		if resource == "f5-spk-egresses" {
			gvrs = append(gvrs, schema.GroupVersionResource{Group: group, Version: "v3", Resource: resource})
		}
	}
	return gvrs
}
