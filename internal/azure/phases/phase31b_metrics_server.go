package phases

import (
	"context"
	"fmt"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

// Phase31bMetricsServer ensures Kubernetes Metrics Server is active for node/pod CPU & memory metrics.
func Phase31bMetricsServer(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 31b] metrics-server: verifying / installing Kubernetes Metrics Server")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 31b] [dry-run] would ensure metrics-server is active")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase31b: build REST config: %w", err)
	}

	cs, err := k8s.NewClientsetFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase31b: creating clientset: %w", err)
	}

	// Check if already present and ready (e.g. AKS built-in metrics-server)
	dep, err := cs.AppsV1().Deployments("kube-system").Get(ctx, "metrics-server", metav1.GetOptions{})
	if err == nil && dep != nil && dep.Status.ReadyReplicas > 0 {
		fmt.Fprintf(os.Stderr, "[phase 31b] metrics-server is already active on AKS (%d ready replicas)\n", dep.Status.ReadyReplicas)
		return nil
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase31b: creating applier: %w", err)
	}

	msBytes, err := manifests.FS.ReadFile("metrics-server/metrics-server.yaml")
	if err != nil {
		return fmt.Errorf("phase31b: reading metrics-server manifest: %w", err)
	}

	objs, err := k8s.ParseYAMLStream(msBytes)
	if err != nil {
		return fmt.Errorf("phase31b: parsing metrics-server manifest: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase31b: applying metrics-server: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 31b] metrics-server: waiting for deployment readiness...")
	if err := k8s.WaitForDeploymentReady(ctx, cs, "kube-system", "metrics-server", 3*time.Minute); err != nil {
		return fmt.Errorf("phase31b: waiting for metrics-server: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 31b] metrics-server OK")
	return nil
}
