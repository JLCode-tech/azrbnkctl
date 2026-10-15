package phases

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/dynamic"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/render"
	"github.com/JLCode-tech/azrbnkctl/internal/netcalc"
)

// Phase37VlansRoutes renders and applies the BNK 2.4 Infra CR (or legacy BNK 2.3 F5SPKVlan/F5SPKStaticRoute CRs).
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

	isLegacy23 := cl != nil && strings.HasPrefix(cl.Bnk.ManifestVersion, "2.3")

	var rendered []byte
	var err error
	crdName := "infras.gateway.k8s.f5.com"

	if isLegacy23 {
		crdName = "f5-spk-vlans.k8s.f5net.com"
		data := render.VlansRoutesData{
			TmmMtu:       cl.Bnk.TmmMtu,
			ExtPrefixLen: extPrefixLen,
			ExtSelfIP:    extSelfIP,
			ExtGwIP:      extGwIP,
			IntPrefixLen: intPrefixLen,
			IntSelfIP:    intSelfIP,
			IntGwIP:      intGwIP,
		}
		rendered, err = render.RenderVlansRoutes(data)
		if err != nil {
			return fmt.Errorf("phase37: rendering vlans and routes: %w", err)
		}
	} else {
		listenerStart, _ := netcalc.Nth(cl.Network.Subnets.External, 100)
		listenerEnd, _ := netcalc.Nth(cl.Network.Subnets.External, 199)
		data := render.InfraData{
			TmmMtu:        cl.Bnk.TmmMtu,
			ExtSelfIP:     extSelfIP,
			ExtGwIP:       extGwIP,
			IntSelfIP:     intSelfIP,
			IntGwIP:       intGwIP,
			ListenerStart: listenerStart,
			ListenerEnd:   listenerEnd,
		}
		rendered, err = render.RenderInfra(data)
		if err != nil {
			return fmt.Errorf("phase37: rendering Infra CR: %w", err)
		}
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

	if err := k8s.WaitForCRDEstablished(ctx, dyn, crdName, 5*time.Minute); err != nil {
		return fmt.Errorf("phase37: waiting for %s CRD: %w", crdName, err)
	}
	fmt.Fprintf(os.Stderr, "[phase 37] %s CRD established\n", crdName)

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
