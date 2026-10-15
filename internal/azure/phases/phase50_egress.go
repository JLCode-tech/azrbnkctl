package phases

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/render"
)

// Phase50Egress renders and applies BNK 2.4 GatewaySettings + EgressGateway (or legacy BNK 2.3 F5SPKEgress) CRs for default egress routing.
func Phase50Egress(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	if cl.Pattern != intent.PatternDualInterface {
		fmt.Fprintln(os.Stderr, "[phase 50] egress: pattern is not dual-interface — skipping")
		return nil
	}

	fmt.Fprintln(os.Stderr, "[phase 50] egress: configuring default egress routing")

	egressNS := cl.Egress.Namespace
	if egressNS == "" {
		egressNS = "bnk-demo"
	}

	var rendered []byte
	var err error
	if strings.HasPrefix(cl.Bnk.ManifestVersion, "2.3") {
		rendered, err = render.RenderEgress(egressNS)
	} else {
		gwcName := cl.Egress.GatewayClassName
		if gwcName == "" {
			gwcName = "f5-gateway-class"
		}
		networkRef := render.InfraTunnelNetwork(cl.HasInternalInterface())
		rendered, err = render.RenderEgressGateway(render.EgressData{
			Namespace:        egressNS,
			GatewayClassName: gwcName,
			NetworkRef:       networkRef,
		})
	}
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

	if cs, csErr := k8s.NewClientsetFromConfig(cfg); csErr == nil {
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: egressNS}}
		if _, createErr := cs.CoreV1().Namespaces().Create(ctx, nsObj, metav1.CreateOptions{}); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("phase50: creating namespace %s: %w", egressNS, createErr)
		}
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
