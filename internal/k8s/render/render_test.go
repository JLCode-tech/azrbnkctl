package render

import (
	"strings"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

func TestRenderCNEInstance(t *testing.T) {
	data := CNEInstanceData{
		ManifestVersion:    "2.3.2",
		DeploymentSize:     "Small",
		TmmCpu:             "4",
		TmmMemory:          "10Gi",
		TmmMtu:             1500,
		ExtPCI:             "0001:00:01.0",
		IntPCI:             "0001:00:02.0",
		Telemetry:          false,
		NetworkAttachments: []string{"tmm-ext", "tmm-int"},
		TmmK8sRoutes:       "10.240.0.0/16",
	}

	out, err := RenderCNEInstance(data)
	if err != nil {
		t.Fatalf("RenderCNEInstance failed: %v", err)
	}

	str := string(out)
	if !strings.Contains(str, "TMM_GENERIC_SOCKET_DRIVER") || !strings.Contains(str, "true") {
		t.Errorf("expected TMM_GENERIC_SOCKET_DRIVER in output, got:\n%s", str)
	}
	if !strings.Contains(str, "hostPath: true") {
		t.Errorf("expected coremon hostPath in output, got:\n%s", str)
	}
	if !strings.Contains(str, "PAL_CPU_SET") {
		t.Errorf("expected PAL_CPU_SET in output, got:\n%s", str)
	}
	if !strings.Contains(str, "TMM_MAPRES_ADDL_VETHS_ON_DP") || !strings.Contains(str, "TRUE") {
		t.Errorf("expected TMM_MAPRES_ADDL_VETHS_ON_DP in output, got:\n%s", str)
	}
	if !strings.Contains(str, "TMM_MAPRES_IFC_CLAIM_LIST") || !strings.Contains(str, "net1,net2") {
		t.Errorf("expected TMM_MAPRES_IFC_CLAIM_LIST with net1,net2 in output, got:\n%s", str)
	}
	if !strings.Contains(str, "USE_GATEWAY_SETTINGS") || !strings.Contains(str, `value: "true"`) {
		t.Errorf("expected USE_GATEWAY_SETTINGS in output, got:\n%s", str)
	}
	if !strings.Contains(str, "MAX_ACTIVE_TMM_REPLICAS") || !strings.Contains(str, `value: "32"`) {
		t.Errorf("expected MAX_ACTIVE_TMM_REPLICAS in output, got:\n%s", str)
	}
	if !strings.Contains(str, "TMM_K8S_ROUTES") || !strings.Contains(str, `value: "10.240.0.0/16"`) {
		t.Errorf("expected TMM_K8S_ROUTES in output, got:\n%s", str)
	}
	if !strings.Contains(str, "- tmm-ext") || !strings.Contains(str, "- tmm-int") {
		t.Errorf("expected network attachments tmm-ext and tmm-int in output, got:\n%s", str)
	}
}

func TestRenderCNEInstance_DefaultRoutesAndAttachments(t *testing.T) {
	data := CNEInstanceData{
		ManifestVersion: "2.4.0",
		DeploymentSize:  "Small",
		TmmMtu:          1500,
	}

	out, err := RenderCNEInstance(data)
	if err != nil {
		t.Fatalf("RenderCNEInstance failed: %v", err)
	}

	str := string(out)
	if strings.Contains(str, "TMM_K8S_ROUTES") {
		t.Errorf("expected no TMM_K8S_ROUTES when empty, got:\n%s", str)
	}
	if !strings.Contains(str, "networkAttachments: []") {
		t.Errorf("expected networkAttachments: [] when empty, got:\n%s", str)
	}
}

func TestRenderLicenseCR(t *testing.T) {
	out, err := RenderLicenseCR("test-jwt-token-12345")
	if err != nil {
		t.Fatalf("RenderLicenseCR failed: %v", err)
	}

	str := string(out)
	if !strings.Contains(str, "jwt: \"test-jwt-token-12345\"") {
		t.Errorf("expected JWT in output, got:\n%s", str)
	}
}

func TestRenderVlansRoutes(t *testing.T) {
	data := VlansRoutesData{
		TmmMtu:       1500,
		ExtPrefixLen: 24,
		ExtSelfIP:    "10.0.17.10",
		ExtGwIP:      "10.0.17.1",
		IntPrefixLen: 24,
		IntSelfIP:    "10.0.18.10",
		IntGwIP:      "10.0.18.1",
	}

	out, err := RenderVlansRoutes(data)
	if err != nil {
		t.Fatalf("RenderVlansRoutes failed: %v", err)
	}

	str := string(out)
	if !strings.Contains(str, "name: external") {
		t.Errorf("expected external VLAN, got:\n%s", str)
	}
	if !strings.Contains(str, "interfaces:\n  - \"1.1\"") {
		t.Errorf("expected interface 1.1 for external VLAN, got:\n%s", str)
	}
	if !strings.Contains(str, "name: internal") {
		t.Errorf("expected internal VLAN, got:\n%s", str)
	}
	if !strings.Contains(str, "interfaces:\n  - \"1.2\"") {
		t.Errorf("expected interface 1.2 for internal VLAN, got:\n%s", str)
	}
	if !strings.Contains(str, "selfip_v4s:\n  - 10.0.17.10") {
		t.Errorf("expected external self IP, got:\n%s", str)
	}
	if !strings.Contains(str, "destination: 10.0.17.1") {
		t.Errorf("expected route-ext-gw, got:\n%s", str)
	}
	if !strings.Contains(str, "interface: external") {
		t.Errorf("expected route-ext-gw interface: external, got:\n%s", str)
	}
	if !strings.Contains(str, "name: route-cluster-svcs") || !strings.Contains(str, "destination: 10.240.0.0") {
		t.Errorf("expected route-cluster-svcs in output, got:\n%s", str)
	}
	if !strings.Contains(str, "name: route-cluster-pods") || !strings.Contains(str, "destination: 10.244.0.0") {
		t.Errorf("expected route-cluster-pods in output, got:\n%s", str)
	}
}

func TestRenderInfra(t *testing.T) {
	data := InfraData{
		TmmMtu:        1500,
		ExtSelfIP:     "10.0.17.10",
		ExtGwIP:       "10.0.17.1",
		IntSelfIP:     "10.0.18.10",
		IntGwIP:       "10.0.18.1",
		ListenerStart: "10.0.17.100",
		ListenerEnd:   "10.0.17.199",
	}

	out, err := RenderInfra(data)
	if err != nil {
		t.Fatalf("RenderInfra failed: %v", err)
	}

	str := string(out)
	for _, want := range []string{
		"kind: Infra",
		"name: infra",
		"namespace: f5-cne-system",
		"name: external-selfip",
		"name: internal-selfip",
		"name: listener-pool",
		`rangeStart: "10.0.17.100"`,
		`rangeEnd: "10.0.17.199"`,
		"name: tmm-ext",
		"name: tmm-int",
		"egressDefaults:",
		"subnet: 192.168.0.0/16",
		"name: route-cluster-pods",
		"name: route-cluster-svcs",
		"name: route-default-egress",
		"nextHop: 10.0.18.1",
	} {
		if !strings.Contains(str, want) {
			t.Errorf("expected %q in RenderInfra output, got:\n%s", want, str)
		}
	}
}

func TestRenderEgressAndEgressGateway(t *testing.T) {
	legacy, err := RenderEgress("bnk-demo")
	if err != nil {
		t.Fatalf("RenderEgress failed: %v", err)
	}
	if !strings.Contains(string(legacy), "kind: F5SPKEgress") || !strings.Contains(string(legacy), "- bnk-demo") {
		t.Errorf("unexpected legacy RenderEgress output:\n%s", string(legacy))
	}

	v24, err := RenderEgressGateway(EgressData{
		Namespace:        "bnk-demo",
		GatewayClassName: "test-aks-gatewayclass",
		NetworkRef:       "internal",
	})
	if err != nil {
		t.Fatalf("RenderEgressGateway failed: %v", err)
	}
	s24 := string(v24)
	for _, want := range []string{
		"kind: GatewaySettings",
		"kind: EgressGateway",
		"name: default-egress",
		"namespace: bnk-demo",
		"gatewayClassName: test-aks-gatewayclass",
		"name: internal",
	} {
		if !strings.Contains(s24, want) {
			t.Errorf("expected %q in RenderEgressGateway output, got:\n%s", want, s24)
		}
	}
}

func TestRenderCNEControllerRBAC(t *testing.T) {
	tmpl, err := manifests.FS.ReadFile("shared/cne-controller-rbac.yaml.tmpl")
	if err != nil {
		t.Fatalf("read shared/cne-controller-rbac.yaml.tmpl: %v", err)
	}
	cl := &intent.Cluster{Metadata: intent.Metadata{Name: "test-aks"}}
	out, err := RenderCNEControllerRBAC(tmpl, cl)
	if err != nil {
		t.Fatalf("RenderCNEControllerRBAC failed: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		`"gateway.k8s.f5.com", "k8s.f5.com"`,
		"name: f5-cne-controller",
		"namespace: f5-cne-system",
		"name: cwc",
		"namespace: f5-utils",
		"namespace: f5-cne-core",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("expected %q in RenderCNEControllerRBAC output, got:\n%s", want, s)
		}
	}
}

func TestRenderCNEInstance_AIGateway241(t *testing.T) {
	azOut, err := RenderCNEInstance(CNEInstanceData{
		ManifestVersion: "2.4.1-3.2850.1-0.0.9",
		DeploymentSize:  "Small",
		TmmMtu:          1500,
	})
	if err != nil {
		t.Fatalf("RenderCNEInstance 2.4.1: %v", err)
	}
	azRendered := string(azOut)
	for _, want := range []string{
		"aiGateway:",
		"enabled: true",
		"aiMetrics: true",
		"loggingSubsystem:\n      enabled: true",
	} {
		if !strings.Contains(azRendered, want) {
			t.Errorf("rendered Azure CNEInstance 2.4.1 missing %q:\n%s", want, azRendered)
		}
	}

	for _, v := range []string{"2.4.1", "v2.4.1-3.2850.1-0.0.9", "2.4.3", "2.5.0", "3.0.0"} {
		if !isAIGatewayManifestVersion(v) {
			t.Errorf("expected isAIGatewayManifestVersion(%q) = true", v)
		}
	}
	for _, v := range []string{"", "2.3.0", "2.4.0", "2.4.0-3.2882.0", "v2.4.0"} {
		if isAIGatewayManifestVersion(v) {
			t.Errorf("expected isAIGatewayManifestVersion(%q) = false", v)
		}
	}
}
