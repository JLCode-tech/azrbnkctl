package phases

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

// Phase32FLO installs the F5 Lifecycle Orchestrator (FLO) operator and CRDs.
func Phase32FLO(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 32] flo: installing F5 Lifecycle Orchestrator")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 32] [dry-run] would apply FLO manifests and CRDs")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase32: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase32: creating applier: %w", err)
	}

	// Ensure cluster issuers (specifically api-server-cluster-issuer) exist before
	// applying the FLO manifest so Certificate/flo-f5-lifecycle-operator-webhook-cert
	// can issue immediately.
	if issuerBytes, iErr := manifests.FS.ReadFile("static/04-issuers.yaml"); iErr == nil {
		if issuerObjs, pErr := k8s.ParseYAMLStream(issuerBytes); pErr == nil {
			_ = applier.ApplyAll(ctx, issuerObjs, true)
		}
	}

	floBytes, err := manifests.FS.ReadFile("flo/flo-manifest.yaml")
	if err != nil {
		return fmt.Errorf("phase32: reading flo manifest: %w", err)
	}

	objs, err := k8s.ParseYAMLStream(floBytes)
	if err != nil {
		return fmt.Errorf("phase32: parsing flo manifest: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase32: applying flo manifests: %w", err)
	}

	dyn, err := k8s.NewDynamicFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase32: creating dynamic client: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 32] flo: waiting for CNEInstance CRD establishment...")
	if err := k8s.WaitForCRDEstablished(ctx, dyn, "cneinstances.k8s.f5.com", 3*time.Minute); err != nil {
		return fmt.Errorf("phase32: waiting for cneinstances CRD: %w", err)
	}

	cs, err := k8s.NewClientsetFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase32: creating clientset: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 32] flo: waiting for flo-f5-lifecycle-operator Deployment readiness...")
	if err := k8s.WaitForDeploymentReady(ctx, cs, "f5-cne-core", "flo-f5-lifecycle-operator", 3*time.Minute); err != nil {
		return fmt.Errorf("phase32: waiting for flo-f5-lifecycle-operator deployment: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 32] FLO OK")
	return nil
}
