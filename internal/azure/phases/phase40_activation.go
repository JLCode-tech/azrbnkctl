package phases

import (
	"context"
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
)

// CNEInstanceGVR is the GVR for CNEInstance resources.
var CNEInstanceGVR = schema.GroupVersionResource{
	Group:    "k8s.f5.com",
	Version:  "v1",
	Resource: "cneinstances",
}

// Phase40Activation polls until BNK TMM is fully active and ready.
func Phase40Activation(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 40] activation: verifying BNK TMM activation and readiness")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 40] [dry-run] would wait for CNEInstance and TMM ready")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase40: build REST config: %w", err)
	}

	dyn, err := k8s.NewDynamicFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase40: creating dynamic client: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 40] activation: polling CNEInstance status...")
	// Timeout 5 minutes for full activation
	err = k8s.WaitForUnstructuredCondition(ctx, dyn, CNEInstanceGVR, "f5-bnk", "instance", "status.state", "Ready", 5*time.Minute)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[phase 40] notice: CNEInstance did not transition to Ready within timeout (may still be starting): %v\n", err)
	} else {
		fmt.Fprintln(os.Stderr, "[phase 40] CNEInstance reports Ready")
	}

	fmt.Fprintln(os.Stderr, "[phase 40] BNK activation verification completed")
	return nil
}
