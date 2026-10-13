package phases

import (
	"context"
	"fmt"
	"os"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/render"
)

// Phase34CNEInstance renders and applies the CNEInstance custom resource.
func Phase34CNEInstance(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 34] cneinstance: rendering and applying CNEInstance CR")

	extPCI := st.Get("TMM_EXT_PCI")
	intPCI := st.Get("TMM_INT_PCI")

	palCpuSet := "0,2"
	if cl.Bnk.TmmCpu == "1" {
		palCpuSet = "0"
	}

	tmmK8sRoutes := "10.240.0.0/16"
	if svcCIDR := st.Get(ServiceCIDRStateKey); svcCIDR != "" {
		tmmK8sRoutes = svcCIDR
	}

	data := render.CNEInstanceData{
		ManifestVersion: cl.Bnk.ManifestVersion,
		DeploymentSize:  cl.Bnk.DeploymentSize,
		TmmCpu:          cl.Bnk.TmmCpu,
		TmmMemory:       cl.Bnk.TmmMemory,
		TmmMtu:          cl.Bnk.TmmMtu,
		ExtPCI:          extPCI,
		IntPCI:          intPCI,
		Telemetry:       cl.Bnk.Telemetry,
		PalCpuSet:       palCpuSet,
		SkipNics:        "eth0",
		TmmK8sRoutes:    tmmK8sRoutes,
	}

	rendered, err := render.RenderCNEInstance(data)
	if err != nil {
		return fmt.Errorf("phase34: rendering CNEInstance: %w", err)
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "[phase 34] [dry-run] rendered CNEInstance (%d bytes)\n", len(rendered))
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase34: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase34: creating applier: %w", err)
	}

	// Ensure NetworkAttachmentDefinition CRD is applied
	if multusBytes, err := manifests.FS.ReadFile("multus/multus-crd.yaml"); err == nil {
		if multusObjs, err := k8s.ParseYAMLStream(multusBytes); err == nil {
			_ = applier.ApplyAll(ctx, multusObjs, true)
		}
	}

	// Apply NetworkAttachmentDefinitions in f5-bnk and default namespaces
	for _, ns := range []string{"f5-bnk", "default"} {
		nadBytes, err := render.RenderNADs(render.NADsData{
			Namespace: ns,
			ExtPCI:    extPCI,
			IntPCI:    intPCI,
		})
		if err != nil {
			return fmt.Errorf("phase34: rendering NADs for %s: %w", ns, err)
		}
		nadObjs, err := k8s.ParseYAMLStream(nadBytes)
		if err != nil {
			return fmt.Errorf("phase34: parsing NADs for %s: %w", ns, err)
		}
		if err := applier.ApplyAll(ctx, nadObjs, true); err != nil {
			return fmt.Errorf("phase34: applying NADs in %s: %w", ns, err)
		}
	}

	objs, err := k8s.ParseYAMLStream(rendered)
	if err != nil {
		return fmt.Errorf("phase34: parsing rendered CNEInstance: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase34: applying CNEInstance: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 34] CNEInstance applied OK")
	return nil
}
