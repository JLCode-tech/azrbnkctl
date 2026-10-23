package phases

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

var gatewayCRDManifests = []string{
	"static/00-gateway-api-crds.yaml",
	"static/01-gie-crds.yaml",
	"static/02-bnk-gateway-crds.yaml",
}

var gatewayEstablishedCRDs = []string{
	"gatewayclasses.gateway.networking.k8s.io",
	"gateways.gateway.networking.k8s.io",
	"httproutes.gateway.networking.k8s.io",
	"grpcroutes.gateway.networking.k8s.io",
	"inferencepools.inference.networking.x-k8s.io",
	"inferencemodelrewrites.inference.networking.x-k8s.io",
	"infras.gateway.k8s.f5.com",
	"l2networks.gateway.k8s.f5.com",
	"l3networks.gateway.k8s.f5.com",
	"gatewaysettings.gateway.k8s.f5.com",
	"egressgateways.gateway.k8s.f5.com",
	"backends.gateway.k8s.f5.com",
}

// Phase36GatewayAPI installs Gateway API, GIE, and BNK 2.4 CRDs, then applies GatewayClasses.
func Phase36GatewayAPI(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	gwcName := cl.Metadata.Name + "-gatewayclass"
	if cl.Metadata.Name == "" {
		gwcName = "f5-cne-gateway-class"
	}
	fmt.Fprintf(os.Stderr, "[phase 36] gateway-api: applying Gateway API/GIE/BNK CRDs and GatewayClasses (f5-gateway-class, %s)\n", gwcName)

	if dryRun {
		fmt.Fprintf(os.Stderr, "[phase 36] [dry-run] would apply Gateway API/GIE/BNK CRDs and GatewayClasses (f5-gateway-class, %s)\n", gwcName)
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

	for _, manifestPath := range gatewayCRDManifests {
		crdBytes, readErr := manifests.FS.ReadFile(manifestPath)
		if readErr != nil {
			return fmt.Errorf("phase36: reading %s: %w", manifestPath, readErr)
		}
		crdObjs, parseErr := k8s.ParseYAMLStream(crdBytes)
		if parseErr != nil {
			return fmt.Errorf("phase36: parsing %s: %w", manifestPath, parseErr)
		}
		if applyErr := applier.ApplyAll(ctx, crdObjs, true); applyErr != nil {
			return fmt.Errorf("phase36: applying %s: %w", manifestPath, applyErr)
		}
	}

	dyn, err := k8s.NewDynamicFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase36: creating dynamic client: %w", err)
	}

	for _, crdName := range gatewayEstablishedCRDs {
		if waitErr := k8s.WaitForCRDEstablished(ctx, dyn, crdName, 2*time.Minute); waitErr != nil {
			return fmt.Errorf("phase36: waiting for CRD %s: %w", crdName, waitErr)
		}
	}

	// Apply canonical f5-gateway-class from static/05-gatewayclass.yaml
	if gwcStaticBytes, readErr := manifests.FS.ReadFile("static/05-gatewayclass.yaml"); readErr == nil {
		if gwcObjs, parseErr := k8s.ParseYAMLStream(gwcStaticBytes); parseErr == nil {
			for _, obj := range gwcObjs {
				if applyErr := applyGatewayClassWithRecreate(ctx, applier, dyn, obj); applyErr != nil {
					return fmt.Errorf("phase36: applying f5-gateway-class: %w", applyErr)
				}
			}
		}
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

	if err := applyGatewayClassWithRecreate(ctx, applier, dyn, gwc); err != nil {
		return fmt.Errorf("phase36: applying GatewayClass %s: %w", gwcName, err)
	}

	fmt.Fprintln(os.Stderr, "[phase 36] GatewayClass OK")
	return nil
}

func applyGatewayClassWithRecreate(ctx context.Context, applier *k8s.Applier, dyn dynamic.Interface, gwc *unstructured.Unstructured) error {
	if _, err := applier.Apply(ctx, gwc, true); err != nil {
		if strings.Contains(err.Error(), "immutable") {
			gwcName := gwc.GetName()
			gvr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
			if existing, getErr := dyn.Resource(gvr).Get(ctx, gwcName, metav1.GetOptions{}); getErr == nil {
				unstructured.RemoveNestedField(existing.Object, "metadata", "finalizers")
				_, _ = dyn.Resource(gvr).Update(ctx, existing, metav1.UpdateOptions{})
				_ = dyn.Resource(gvr).Delete(ctx, gwcName, metav1.DeleteOptions{})
			}
			if _, err2 := applier.Apply(ctx, gwc, true); err2 == nil {
				return nil
			}
		}
		return err
	}
	return nil
}
