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
)

// Phase33CWCCerts creates ClusterIssuers and the ext-ca certificate.
func Phase33CWCCerts(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 33] certs: creating cluster issuers and ext-ca")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 33] [dry-run] would apply 04-issuers.yaml")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase33: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase33: creating applier: %w", err)
	}

	if err := applyClusterIssuers(ctx, applier, cl); err != nil {
		return fmt.Errorf("phase33: applying issuers: %w", err)
	}

	dyn, err := k8s.NewDynamicFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase33: creating dynamic client: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 33] certs: waiting for ext-ca Certificate readiness...")
	if err := k8s.WaitForCertificateReady(ctx, dyn, "cert-manager", "ext-ca", 2*time.Minute); err != nil {
		return fmt.Errorf("phase33: waiting for ext-ca certificate: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 33] certs OK")
	return nil
}
