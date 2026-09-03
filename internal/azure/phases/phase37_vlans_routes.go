package phases

import (
	"context"
	"fmt"
	"os"
	"time"

	"k8s.io/client-go/dynamic"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/render"
	"github.com/JLCode-tech/azrbnkctl/internal/netcalc"
)

// Phase37VlansRoutes renders and applies F5SPKVlan and F5SPKStaticRoute CRs.
func Phase37VlansRoutes(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 37] vlans-routes: configuring dataplane VLANs and static routes")

	extPrefixLen, _ := netcalc.CIDRPrefixLen(cl.Network.Subnets.External)
	extSelfIP := st.Get("AZURE_EXT_SELF_IP")
	extGwIP := st.Get("AZURE_EXT_GW_IP")

	intPrefixLen := 0
	intSelfIP := ""
	intGwIP := ""

	if cl.Pattern == intent.PatternDualInterface && cl.Network.Subnets.Internal != "" {
		intPrefixLen, _ = netcalc.CIDRPrefixLen(cl.Network.Subnets.Internal)
		intSelfIP = st.Get("AZURE_INT_SELF_IP")
		intGwIP = st.Get("AZURE_INT_GW_IP")
	}

	data := render.VlansRoutesData{
		TmmMtu:       cl.Bnk.TmmMtu,
		ExtPrefixLen: extPrefixLen,
		ExtSelfIP:    extSelfIP,
		ExtGwIP:      extGwIP,
		IntPrefixLen: intPrefixLen,
		IntSelfIP:    intSelfIP,
		IntGwIP:      intGwIP,
	}

	rendered, err := render.RenderVlansRoutes(data)
	if err != nil {
		return fmt.Errorf("phase37: rendering vlans and routes: %w", err)
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "[phase 37] [dry-run] rendered VLANs and routes (%d bytes)\n", len(rendered))
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase37: build REST config: %w", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase37: dynamic client: %w", err)
	}

	if err := k8s.WaitForCRDEstablished(ctx, dyn, "f5-spk-vlans.k8s.f5net.com", 5*time.Minute); err != nil {
		return fmt.Errorf("phase37: waiting for f5-spk-vlans.k8s.f5net.com CRD: %w", err)
	}
	fmt.Fprintln(os.Stderr, "[phase 37] f5-spk-vlans.k8s.f5net.com CRD established")

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase37: creating applier: %w", err)
	}

	objs, err := k8s.ParseYAMLStream(rendered)
	if err != nil {
		return fmt.Errorf("phase37: parsing rendered VLANs and routes: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase37: applying VLANs and routes: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 37] VLANs and static routes applied OK")
	return nil
}
