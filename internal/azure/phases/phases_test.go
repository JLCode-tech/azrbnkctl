package phases

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

func TestIsAzureNotFound(t *testing.T) {
	notFoundErr := &azcore.ResponseError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  "ResourceNotFound",
	}
	if !isAzureNotFound(notFoundErr) {
		t.Errorf("expected 404 ResourceNotFound to be classified as not found")
	}

	scopeLockedErr := &azcore.ResponseError{
		StatusCode: http.StatusConflict,
		ErrorCode:  "ScopeLocked",
	}
	if isAzureNotFound(scopeLockedErr) {
		t.Errorf("expected 409 ScopeLocked to NOT be classified as not found")
	}
}

func TestPhases_DryRun(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "azrbnkctl-phases-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	manifestPath := filepath.Join(tmpDir, "cluster.yaml")
	manifestContent := `apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: test-aks
  subscriptionId: 00000000-0000-0000-0000-000000000000
  location: eastus2
pattern: dual-interface
network:
  vnetCidr: 10.0.0.0/16
  subnets:
    system: 10.0.0.0/20
    public: 10.0.16.0/24
    external: 10.0.17.0/24
    internal: 10.0.18.0/24
cluster:
  kubernetesVersion: "1.32.0"
  systemNodePool:
    vmSize: Standard_D4s_v5
    count: 2
  tmmNodePool:
    vmSize: Standard_D8s_v5
    count: 1
bnk:
  manifestVersion: "2.3.2"
  farArchive: "fake-far.tar.gz"
  jwt: "fake-jwt"
`
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}

	cl, err := intent.Load(manifestPath)
	if err != nil {
		t.Fatalf("intent.Load failed: %v", err)
	}

	st, err := state.New(tmpDir)
	if err != nil {
		t.Fatalf("state.New failed: %v", err)
	}

	ctx := context.Background()
	clients := &azure.Clients{}
	dryRun := true

	// Phase 00
	if err := Phase00Preflight(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase00 failed: %v", err)
	}

	// Phase 10
	if err := Phase10ResourceGroup(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase10 failed: %v", err)
	}
	if st.Get("AZURE_RESOURCE_GROUP") != "azrbnkctl-test-aks-rg" {
		t.Errorf("expected AZURE_RESOURCE_GROUP, got %s", st.Get("AZURE_RESOURCE_GROUP"))
	}

	// Phase 11
	if err := Phase11NSG(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase11 failed: %v", err)
	}

	// Phase 12
	if err := Phase12VNet(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase12 failed: %v", err)
	}

	// Phase 13
	if err := Phase13NatGateway(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase13 failed: %v", err)
	}

	// Phase 14
	if err := Phase14PublicIPs(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase14 failed: %v", err)
	}

	// Phase 21
	if err := Phase21AKSCluster(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase21 failed: %v", err)
	}

	// Phase 22
	if err := Phase22TMMNodePool(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase22 failed: %v", err)
	}

	// Phase 23
	if err := Phase23Kubeconfig(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase23 failed: %v", err)
	}

	// Phase 24
	if err := Phase24NodeLabels(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase24 failed: %v", err)
	}

	// Phase 25
	if err := Phase25Discovery(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase25 failed: %v", err)
	}
	if st.Get("AZURE_EXT_GW_IP") != "10.0.17.1" {
		t.Errorf("expected AZURE_EXT_GW_IP 10.0.17.1, got %s", st.Get("AZURE_EXT_GW_IP"))
	}

	// Phase 30
	if err := Phase30Namespaces(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase30 failed: %v", err)
	}

	// Phase 31
	if err := Phase31CertManager(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase31 failed: %v", err)
	}

	// Phase 32
	if err := Phase32FLO(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase32 failed: %v", err)
	}

	// Phase 33
	if err := Phase33CWCCerts(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase33 failed: %v", err)
	}

	// Phase 34
	if err := Phase34CNEInstance(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase34 failed: %v", err)
	}

	// Phase 35
	if err := Phase35License(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase35 failed: %v", err)
	}

	// Phase 36
	if err := Phase36GatewayAPI(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase36 failed: %v", err)
	}

	// Phase 37
	if err := Phase37VlansRoutes(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase37 failed: %v", err)
	}

	// Phase 38
	if err := Phase38CWCHeal(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase38 failed: %v", err)
	}

	// Phase 38b
	if err := Phase38bDSSMInsecureOverlay(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase38b failed: %v", err)
	}

	// Phase 38c
	if err := Phase38cPodManagerHeal(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase38c failed: %v", err)
	}

	// Phase 38d
	if err := Phase38dTMMLogStream(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase38d failed: %v", err)
	}

	// Phase 40
	if err := Phase40Activation(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase40 failed: %v", err)
	}

	// Phase 50
	if err := Phase50Egress(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase50 failed: %v", err)
	}

	// Down phases
	if err := PhaseDownCluster(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("PhaseDownCluster failed: %v", err)
	}
	if err := PhaseDownNAT(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("PhaseDownNAT failed: %v", err)
	}
	if err := PhaseDownNetworks(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("PhaseDownNetworks failed: %v", err)
	}
	if err := PhaseDownResourceGroup(ctx, cl, st, clients, false, dryRun); err != nil {
		t.Fatalf("PhaseDownResourceGroup (keep default) failed: %v", err)
	}
	if err := PhaseDownResourceGroup(ctx, cl, st, clients, true, dryRun); err != nil {
		t.Fatalf("PhaseDownResourceGroup (deleteRG=true) failed: %v", err)
	}

	// Also verify BNK 2.4 path for Phase 37 and Phase 50
	cl.Bnk.ManifestVersion = "2.4.0"
	if err := Phase37VlansRoutes(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase37 (BNK 2.4) failed: %v", err)
	}
	if err := Phase50Egress(ctx, cl, st, clients, dryRun); err != nil {
		t.Fatalf("Phase50 (BNK 2.4) failed: %v", err)
	}
}
