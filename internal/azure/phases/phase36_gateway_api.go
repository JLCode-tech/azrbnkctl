package phases

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
)

// Phase36GatewayAPI applies the GatewayClass for F5 BNK Gateway API.
func Phase36GatewayAPI(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 36] gateway-api: applying GatewayClass f5-cne-gateway-class")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 36] [dry-run] would apply GatewayClass f5-cne-gateway-class")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase36: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase36: creating applier: %w", err)
	}

	gwc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "GatewayClass",
			"metadata": map[string]interface{}{
				"name": "f5-cne-gateway-class",
			},
			"spec": map[string]interface{}{
				"controllerName": "f5.com/f5-bnk-f5-cne-controller",
			},
		},
	}

	if _, err := applier.Apply(ctx, gwc, true); err != nil {
		return fmt.Errorf("phase36: applying GatewayClass: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 36] GatewayClass OK")
	return nil
}
