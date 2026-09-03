package phases

import (
	"context"
	"fmt"
	"os"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/render"
)

// Phase50Egress renders and applies F5SPKEgress CRs for default egress routing.
func Phase50Egress(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	if cl.Pattern != intent.PatternDualInterface {
		fmt.Fprintln(os.Stderr, "[phase 50] egress: pattern is not dual-interface — skipping")
		return nil
	}

	fmt.Fprintln(os.Stderr, "[phase 50] egress: configuring default egress routing")

	rendered, err := render.RenderEgress("egress-demo")
	if err != nil {
		return fmt.Errorf("phase50: rendering egress CR: %w", err)
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "[phase 50] [dry-run] rendered Egress CR (%d bytes)\n", len(rendered))
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase50: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase50: creating applier: %w", err)
	}

	objs, err := k8s.ParseYAMLStream(rendered)
	if err != nil {
		return fmt.Errorf("phase50: parsing egress CR: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase50: applying egress CR: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 50] Egress CR applied OK")
	return nil
}
