package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/forge"
)

func TestResolveBenchmarkContext_FromWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	wsName := "test-bench-ws"
	wsDir := filepath.Join(tmpDir, ".azrbnkctl", wsName)
	if err := os.MkdirAll(wsDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 1. Write state.env
	st, err := state.Load(wsDir)
	if err != nil {
		t.Fatalf("state.Load(): %v", err)
	}
	st.Set("REGION", "ap-southeast-2")
	st.Set("JUMPHOST_INSTANCE_ID", "i-0abcd1234ef567890")
	st.Set("JUMPHOST_BNK_EXT_ENI_IP", "10.0.10.20")
	st.Set("DEMO_VIP_HTTPROUTE", "10.0.10.100")
	if err := st.Save(); err != nil {
		t.Fatalf("st.Save(): %v", err)
	}

	// 2. Write forge_link.json
	link := &forge.Link{
		ForgeURL:    "http://forge.example.internal:8000",
		ForgeMCPURL: "http://forge.example.internal:8001",
		ClusterID:   99,
		Status:      "registered",
	}
	if err := forge.WriteLink(wsDir, link); err != nil {
		t.Fatalf("forge.WriteLink(): %v", err)
	}

	// Change working dir temporarily so .azrbnkctl/ matches
	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	// Clear flags
	flagBenchRegion = ""
	flagBenchInstanceID = ""
	flagBenchSourceIP = ""
	flagBenchVIP = ""
	flagBenchModel = ""
	flagBenchForgeURL = ""
	flagBenchWorkspace = wsName
	flagBenchConfig = ""

	err = resolveBenchmarkContext(benchmarkCmd)
	if err != nil {
		t.Fatalf("resolveBenchmarkContext failed: %v", err)
	}

	if flagBenchRegion != "ap-southeast-2" {
		t.Errorf("flagBenchRegion = %q, want 'ap-southeast-2'", flagBenchRegion)
	}
	if flagBenchInstanceID != "i-0abcd1234ef567890" {
		t.Errorf("flagBenchInstanceID = %q, want 'i-0abcd1234ef567890'", flagBenchInstanceID)
	}
	if flagBenchSourceIP != "10.0.10.20" {
		t.Errorf("flagBenchSourceIP = %q, want '10.0.10.20'", flagBenchSourceIP)
	}
	if flagBenchVIP != "10.0.10.100" {
		t.Errorf("flagBenchVIP = %q, want '10.0.10.100'", flagBenchVIP)
	}
	if flagBenchForgeURL != "http://forge.example.internal:8000" {
		t.Errorf("flagBenchForgeURL = %q, want 'http://forge.example.internal:8000'", flagBenchForgeURL)
	}
}

func TestResolveBenchmarkContext_SyntheticCluster(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "cluster.yaml")
	cfgContent := `apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: test-synth-cluster
  location: eastus2
network:
  vnetCidr: 10.0.0.0/16
  subnets:
    system: 10.0.0.0/20
    external: 10.0.10.0/24
pattern: dual-interface
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	flagBenchRegion = ""
	flagBenchInstanceID = ""
	flagBenchSourceIP = ""
	flagBenchVIP = ""
	flagBenchModel = ""
	flagBenchForgeURL = ""
	flagBenchWorkspace = ""
	flagBenchConfig = cfgPath
	flagBenchSynthetic = true

	err := resolveBenchmarkContext(benchmarkCmd)
	if err != nil {
		t.Fatalf("resolveBenchmarkContext failed: %v", err)
	}

	if !flagBenchSynthetic {
		t.Error("flagBenchSynthetic = false, want true")
	}
	if flagBenchModel != "llama3" {
		t.Errorf("flagBenchModel = %q, want 'llama3'", flagBenchModel)
	}
	if flagBenchVIP != "10.0.10.100" {
		t.Errorf("flagBenchVIP = %q, want '10.0.10.100'", flagBenchVIP)
	}
	if flagBenchRegion != "eastus2" {
		t.Errorf("flagBenchRegion = %q, want 'eastus2'", flagBenchRegion)
	}
}

func TestNLBOptInTags_Synthetic(t *testing.T) {
	flagBenchSynthetic = true
	defer func() { flagBenchSynthetic = false }()

	tags := nlbOptInTags("", "vllm", "80", "")
	if tags == nil {
		t.Fatal("expected tags to be non-nil when synthetic is true")
	}
	if tags["synthetic"] != "true" {
		t.Errorf("tags[synthetic] = %q, want 'true'", tags["synthetic"])
	}
	if tags["simulator"] != "llm-d-inference-sim" {
		t.Errorf("tags[simulator] = %q, want 'llm-d-inference-sim'", tags["simulator"])
	}
}

func TestResolveForgeGraph_ClusterScanAndProxyDiscovery(t *testing.T) {
	// Setup workspace with forge_link.json
	tmpDir := t.TempDir()
	wsName := "test-scan-ws"
	wsDir := filepath.Join(tmpDir, ".azrbnkctl", wsName)
	if err := os.MkdirAll(wsDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := &forge.Link{
		ForgeURL:  "http://127.0.0.1:8000",
		ClusterID: 42,
		Status:    "registered",
	}
	if err := forge.WriteLink(wsDir, link); err != nil {
		t.Fatalf("forge.WriteLink(): %v", err)
	}

	oldWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer func() { _ = os.Chdir(oldWd) }()

	// Save/restore seams
	oldAgent := registerBenchmarkAgentFn
	oldDiscoverTargets := discoverTargetsFn
	oldListTargets := listBenchmarkTargetsFn
	oldDiscoverProxies := discoverProxiesFn
	oldListProxies := listProxyDeploymentsFn
	defer func() {
		registerBenchmarkAgentFn = oldAgent
		discoverTargetsFn = oldDiscoverTargets
		listBenchmarkTargetsFn = oldListTargets
		discoverProxiesFn = oldDiscoverProxies
		listProxyDeploymentsFn = oldListProxies
	}()

	agentCalled := false
	registerBenchmarkAgentFn = func(ctx context.Context, opts forge.BenchmarkAgentOptions) (forge.BenchmarkAgentResponse, error) {
		agentCalled = true
		return forge.BenchmarkAgentResponse{ID: 10, Name: opts.Name}, nil
	}

	discoverTargetsCalled := false
	discoverTargetsFn = func(ctx context.Context, opts forge.DiscoverTargetsOptions) (forge.DiscoverTargetsResponse, error) {
		discoverTargetsCalled = true
		if opts.ClusterID != 42 {
			t.Errorf("opts.ClusterID = %d, want 42", opts.ClusterID)
		}
		return forge.DiscoverTargetsResponse{
			ClusterID:       42,
			DiscoveredCount: 1,
			CreatedTargets: []forge.CreatedTargetItem{
				{ID: 2, Name: "vllm-azrbnkctl-scn-aiinference", Status: "created"},
			},
		}, nil
	}

	listTargetsCalled := false
	listBenchmarkTargetsFn = func(ctx context.Context, opts forge.ListBenchmarkTargetsOptions) ([]forge.BenchmarkTargetResponse, error) {
		listTargetsCalled = true
		return []forge.BenchmarkTargetResponse{
			{
				ID:           2,
				Name:         "vllm-azrbnkctl-scn-aiinference",
				ClusterID:    42,
				LLMBaseURL:   "http://vllm.azrbnkctl-scn-aiinference:80",
				LLMModel:     "meta-llama/Llama-3.1-8B-Instruct",
				LLMNamespace: "azrbnkctl-scn-aiinference",
			},
		}, nil
	}

	discoverProxiesCalled := false
	discoverProxiesFn = func(ctx context.Context, opts forge.ProxyDiscoverOptions) (forge.ProxyDiscoveryResult, error) {
		discoverProxiesCalled = true
		if opts.TargetID != 2 {
			t.Errorf("opts.TargetID = %d, want 2", opts.TargetID)
		}
		return forge.ProxyDiscoveryResult{TargetID: 2, DiscoveredCount: 1}, nil
	}

	listProxiesCalled := false
	listProxyDeploymentsFn = func(ctx context.Context, opts forge.ProxyDiscoverOptions) ([]forge.ProxyDeployment, error) {
		listProxiesCalled = true
		return []forge.ProxyDeployment{
			{
				ID:          1,
				TargetID:    2,
				ProxyType:   "f5-bnk",
				ExternalURL: "http://10.10.10.108:80",
				Status:      "discovered",
			},
		}, nil
	}

	flagBenchWorkspace = wsName
	flagBenchConfig = ""
	flagBenchInstanceID = "i-test99"
	flagBenchSourceIP = "10.0.1.50"
	flagBenchForgeURL = "http://127.0.0.1:8000"
	flagBenchModel = "llama3"
	flagBenchProxy = "f5-bnk"
	flagBenchVIP = ""

	graph := resolveForgeGraph(context.Background(), forge.RestCreds{}, "azrbnkctl-jumphost-i-test99")

	if !agentCalled {
		t.Error("expected registerBenchmarkAgentFn to be called")
	}
	if !discoverTargetsCalled {
		t.Error("expected discoverTargetsFn to be called")
	}
	if !listTargetsCalled {
		t.Error("expected listBenchmarkTargetsFn to be called")
	}
	if !discoverProxiesCalled {
		t.Error("expected discoverProxiesFn to be called")
	}
	if !listProxiesCalled {
		t.Error("expected listProxyDeploymentsFn to be called")
	}

	if graph.targetID != 2 {
		t.Errorf("graph.targetID = %d, want 2", graph.targetID)
	}
	if graph.proxyDeploymentID != 1 {
		t.Errorf("graph.proxyDeploymentID = %d, want 1", graph.proxyDeploymentID)
	}
	if flagBenchVIP != "10.10.10.108" {
		t.Errorf("flagBenchVIP = %q, want '10.10.10.108'", flagBenchVIP)
	}
}
