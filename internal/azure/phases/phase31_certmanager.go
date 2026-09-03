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

// Phase31CertManager installs cert-manager and waits for readiness.
func Phase31CertManager(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 31] cert-manager: installing cert-manager v1.16.1")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 31] [dry-run] would apply cert-manager manifests")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase31: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase31: creating applier: %w", err)
	}

	cmBytes, err := manifests.FS.ReadFile("cert-manager/cert-manager-v1.16.1.yaml")
	if err != nil {
		return fmt.Errorf("phase31: reading cert-manager manifest: %w", err)
	}

	objs, err := k8s.ParseYAMLStream(cmBytes)
	if err != nil {
		return fmt.Errorf("phase31: parsing cert-manager manifest: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase31: applying cert-manager: %w", err)
	}

	multusBytes, err := manifests.FS.ReadFile("multus/multus-crd.yaml")
	if err == nil {
		if multusObjs, err := k8s.ParseYAMLStream(multusBytes); err == nil {
			_ = applier.ApplyAll(ctx, multusObjs, true)
		}
	}

	cs, err := k8s.NewClientsetFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase31: creating clientset: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 31] cert-manager: waiting for webhook readiness...")
	if err := k8s.WaitForDeploymentReady(ctx, cs, "cert-manager", "cert-manager-webhook", 3*time.Minute); err != nil {
		return fmt.Errorf("phase31: waiting for cert-manager-webhook: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 31] cert-manager OK")
	return nil
}
