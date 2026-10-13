package cli

import (
	"strings"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

func TestRenderManifest(t *testing.T) {
	st, _ := state.Load(t.TempDir())
	st.Set("NODE_PRIMARY_IFNAME", "eth0")
	cl := &intent.Cluster{Network: intent.Network{
		VNetCIDR: "10.0.0.0/16",
		Subnets: intent.Subnets{
			External: "10.0.10.0/24",
		},
	}}

	out, err := renderManifest("nodeInterfaceName: {{.NodeIfname}}\ngateway: {{.ExtGateway}}\ndestination: {{.VpcNet}}/{{.VpcPrefixLen}}\n", cl, st)
	if err != nil {
		t.Fatalf("renderManifest: %v", err)
	}
	for _, want := range []string{"nodeInterfaceName: eth0", "gateway: 10.0.10.1", "destination: 10.0.0.0/16"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q:\n%s", want, out)
		}
	}

	plain := "kind: Namespace\nmetadata: {name: x}\n"
	if got, _ := renderManifest(plain, cl, st); got != plain {
		t.Errorf("content without directives must pass through unchanged, got %q", got)
	}
	if _, err := renderManifest("{{.Unclosed", cl, st); err == nil {
		t.Error("malformed template should error")
	}
}
