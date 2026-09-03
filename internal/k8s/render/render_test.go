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
	if !strings.Contains(str, "PCIDEVICE_INTEL_COM_EXT") || !strings.Contains(str, "0001:00:01.0") {
		t.Errorf("expected ExtPCI in output, got:\n%s", str)
	}
	if !strings.Contains(str, "PCIDEVICE_INTEL_COM_INT") || !strings.Contains(str, "0001:00:02.0") {
		t.Errorf("expected IntPCI in output, got:\n%s", str)
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
}
