package phases

import (
	"context"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
)

// Phase36GatewayAPI applies the GatewayClass for F5 BNK Gateway API.
func Phase36GatewayAPI(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	gwcName := cl.Metadata.Name + "-gatewayclass"
	if cl.Metadata.Name == "" {
		gwcName = "f5-cne-gateway-class"
	}
	fmt.Fprintf(os.Stderr, "[phase 36] gateway-api: applying GatewayClass %s\n", gwcName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[phase 36] [dry-run] would apply GatewayClass %s\n", gwcName)
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
				"name": gwcName,
			},
			"spec": map[string]interface{}{
				"controllerName": "f5.com/f5-cne-system-f5-cne-controller",
				"description":    "F5 BIG-IP Kubernetes Gateway",
			},
		},
	}

	if _, err := applier.Apply(ctx, gwc, true); err != nil {
		if strings.Contains(err.Error(), "immutable") {
			dyn, dynErr := k8s.NewDynamicFromConfig(cfg)
			if dynErr == nil {
				gvr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
				if existing, getErr := dyn.Resource(gvr).Get(ctx, gwcName, metav1.GetOptions{}); getErr == nil {
					unstructured.RemoveNestedField(existing.Object, "metadata", "finalizers")
					_, _ = dyn.Resource(gvr).Update(ctx, existing, metav1.UpdateOptions{})
					_ = dyn.Resource(gvr).Delete(ctx, gwcName, metav1.DeleteOptions{})
				}
				if _, err2 := applier.Apply(ctx, gwc, true); err2 == nil {
					fmt.Fprintln(os.Stderr, "[phase 36] GatewayClass OK (recreated)")
					return nil
				}
			}
		}
		return fmt.Errorf("phase36: applying GatewayClass: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 36] GatewayClass OK")
	return nil
}
