package scenarios

import (
	"net"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/netcalc"
)

// NetVars are the cluster-derived network values templated manifests can
// reference — scenario manifests and `k apply --config` examples alike — so
// no example needs a hardcoded interface name, gateway, or SelfIP.
type NetVars struct {
	// NodeIfname is the worker's primary NIC (state NODE_PRIMARY_IFNAME, phase 17c).
	NodeIfname string
	// VpcNet / VpcPrefixLen split network.vpcCidr.
	VpcNet       string
	VpcPrefixLen int
	// ExtGateway / IntGateway are the subnet routers (first host address)
	// of dataPath.external / dataPath.internal. IntGateway is empty on
	// single-interface patterns.
	ExtGateway string
	IntGateway string
	// TMMExtSelfIP / TMMIntSelfIP come from state (phase 17) with the intent's
	// derived values as fallback.
	TMMExtSelfIP string
	TMMIntSelfIP string
	// VIP and AgentCoreVIP are derived VIP addresses on the external subnet.
	VIP          string
	AgentCoreVIP string
}

// NetVarsFor derives NetVars from cluster.yaml and state.env; either may be nil.
func NetVarsFor(cl *intent.Cluster, st *state.State) NetVars {
	var v NetVars
	if st != nil {
		v.NodeIfname = st.Get("NODE_PRIMARY_IFNAME")
		if v.NodeIfname == "" {
			v.NodeIfname = st.Get("TMM_EXT_IFACE")
		}
		v.TMMExtSelfIP = st.Get("TMM_EXT_SELFIP")
		v.TMMIntSelfIP = st.Get("TMM_INT_SELFIP")
		v.VIP = st.Get("GATEWAY_VIP")
	}
	if cl == nil {
		return v
	}
	if v.NodeIfname == "" {
		v.NodeIfname = "eth1"
	}
	v.VpcNet, v.VpcPrefixLen = SplitCIDR(cl.Network.VNetCIDR)
	if cl.Network.Subnets.External != "" {
		v.ExtGateway = SubnetGateway(cl.Network.Subnets.External)
		if v.VIP == "" {
			if vip, err := cl.DefaultVIP(); err == nil {
				v.VIP = vip
			}
		}
		if ip, err := netcalc.Nth(cl.Network.Subnets.External, 107); err == nil {
			v.AgentCoreVIP = ip
		}
	}
	if cl.Pattern == intent.PatternDualInterface && cl.Network.Subnets.Internal != "" {
		v.IntGateway = SubnetGateway(cl.Network.Subnets.Internal)
	}
	return v
}

// TemplateVars is the data `k apply --config` renders example manifests with:
// every state.env key by name plus the NetVars fields.
func TemplateVars(cl *intent.Cluster, st *state.State) map[string]any {
	m := map[string]any{}
	if st != nil {
		for k, val := range st.All() {
			m[k] = val
		}
	}
	nv := NetVarsFor(cl, st)
	m["NodeIfname"] = nv.NodeIfname
	m["VpcNet"], m["VpcPrefixLen"] = nv.VpcNet, nv.VpcPrefixLen
	m["ExtGateway"], m["IntGateway"] = nv.ExtGateway, nv.IntGateway
	m["TMMExtSelfIP"], m["TMMIntSelfIP"] = nv.TMMExtSelfIP, nv.TMMIntSelfIP
	m["VIP"], m["AgentCoreVIP"] = nv.VIP, nv.AgentCoreVIP
	return m
}

// SplitCIDR returns the network address and prefix length of cidr ("" / 0 if invalid).
func SplitCIDR(cidr string) (string, int) {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", 0
	}
	ones, _ := n.Mask.Size()
	return n.IP.String(), ones
}

// SubnetGateway is the AWS subnet router: the first usable address of the CIDR.
func SubnetGateway(cidr string) string {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return ""
	}
	gw := make(net.IP, len(n.IP))
	copy(gw, n.IP)
	gw[len(gw)-1]++
	return gw.String()
}
