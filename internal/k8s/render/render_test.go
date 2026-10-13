package render

import (
	"strings"
	"testing"
)

func TestRenderCNEInstance(t *testing.T) {
	data := CNEInstanceData{
		ManifestVersion: "2.3.2",
		DeploymentSize:  "Small",
		TmmCpu:          "4",
		TmmMemory:       "10Gi",
		TmmMtu:          1500,
		ExtPCI:          "0001:00:01.0",
		IntPCI:          "0001:00:02.0",
		Telemetry:       false,
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
	if !strings.Contains(str, "selfip_v4s:\n  - 10.0.17.10") {
		t.Errorf("expected external self IP, got:\n%s", str)
	}
	if !strings.Contains(str, "destination: 10.0.17.1") {
		t.Errorf("expected route-ext-gw, got:\n%s", str)
	}
	if !strings.Contains(str, "name: route-cluster-svcs") || !strings.Contains(str, "destination: 10.240.0.0") {
		t.Errorf("expected route-cluster-svcs in output, got:\n%s", str)
	}
	if !strings.Contains(str, "name: route-cluster-pods") || !strings.Contains(str, "destination: 10.244.0.0") {
		t.Errorf("expected route-cluster-pods in output, got:\n%s", str)
	}
}
