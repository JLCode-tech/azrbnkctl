package scenarios_test

import (
	"strings"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/scenarios"
)

func makeTestCluster() *intent.Cluster {
	return &intent.Cluster{
		Metadata: intent.Metadata{
			Name:     "syd-tracer",
			Location: "australiaeast",
		},
		Network: intent.Network{
			VNetCIDR: "10.0.0.0/16",
			Subnets: intent.Subnets{
				System:   "10.0.1.0/24",
				External: "10.0.10.0/24",
				Internal: "10.0.20.0/24",
			},
		},
		Pattern: "host-device",
	}
}

func makeTestState(t *testing.T) *state.State {
	t.Helper()
	st, _ := state.Load(t.TempDir())
	st.Set("JUMPHOST_INSTANCE_ID", "i-0abcdef1234567890")
	st.Set("JUMPHOST_BNK_EXT_ENI_IP", "10.0.10.5")
	return st
}

func TestRender_StaticOnly(t *testing.T) {
	cl := makeTestCluster()
	st := makeTestState(t)

	in := scenarios.EnvDiagramInput{
		Cluster:  cl,
		State:    st,
		Scenario: "http-routing-e2e",
		VIP:      "10.0.10.100",
	}
	out := scenarios.Render(in)
	if out == "" {
		t.Fatal("Render returned empty string")
	}
	checks := []string{
		"syd-tracer",
		"australiaeast",
		"10.0.10.0/24",
		"10.0.10.100",
		"i-0abcdef1234567890",
		"10.0.10.5",
		"http-routing-e2e",
	}

	for _, want := range checks {
		if !strings.Contains(out, want) {
			t.Errorf("Render output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestRender_NilCluster(t *testing.T) {
	in := scenarios.EnvDiagramInput{
		Cluster: nil,
		State:   nil,
	}
	out := scenarios.Render(in)
	// Should not panic and should contain "(unknown)" markers.
	if !strings.Contains(out, "(unknown)") {
		t.Errorf("expected (unknown) in nil-cluster output, got:\n%s", out)
	}
}

func TestRender_NilState(t *testing.T) {
	cl := makeTestCluster()
	in := scenarios.EnvDiagramInput{
		Cluster: cl,
		State:   nil,
		VIP:     "10.0.10.100",
	}
	out := scenarios.Render(in)
	if !strings.Contains(out, "(unknown)") {
		t.Errorf("expected (unknown) for nil state, got:\n%s", out)
	}
	// Cluster data should still be present.
	if !strings.Contains(out, "syd-tracer") {
		t.Errorf("expected cluster name in output, got:\n%s", out)
	}
}

func TestRender_NilClients_LiveFieldsFallback(t *testing.T) {
	cl := makeTestCluster()
	st := makeTestState(t)
	in := scenarios.EnvDiagramInput{
		Cluster:   cl,
		State:     st,
		Clientset: nil, // no live reads
		Dynamic:   nil,
		VIP:       "10.0.10.100",
	}
	out := scenarios.Render(in)
	// Live fields should fall back to "(unknown)".
	unknownCount := strings.Count(out, "(unknown)")
	// tmmPodIP + gatewayAddr + httprouteAccepted = 3 live fields, each "(unknown)" at least once.
	if unknownCount < 3 {
		t.Errorf("expected at least 3 (unknown) for nil clients, got %d in:\n%s", unknownCount, out)
	}
}

func TestRender_ScenarioFooter(t *testing.T) {
	cl := makeTestCluster()
	st := makeTestState(t)
	in := scenarios.EnvDiagramInput{
		Cluster:  cl,
		State:    st,
		Scenario: "my-test-scenario",
		VIP:      "10.0.10.100",
	}
	out := scenarios.Render(in)
	if !strings.Contains(out, "my-test-scenario") {
		t.Errorf("scenario name not in diagram:\n%s", out)
	}
}

func TestRender_NoScenarioFooter(t *testing.T) {
	cl := makeTestCluster()
	st := makeTestState(t)
	in := scenarios.EnvDiagramInput{
		Cluster:  cl,
		State:    st,
		Scenario: "", // no scenario footer
		VIP:      "10.0.10.100",
	}
	out := scenarios.Render(in)
	if strings.Contains(out, "scenario:") {
		t.Errorf("unexpected scenario: line when Scenario='': %s", out)
	}
}
