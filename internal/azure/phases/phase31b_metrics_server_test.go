package phases

import (
	"context"
	"testing"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

func TestPhase31bMetricsServer_DryRun(t *testing.T) {
	cl := &intent.Cluster{
		Metadata: intent.Metadata{
			Name:     "test-cluster",
			Location: "australiaeast",
		},
	}
	st, _ := state.New(t.TempDir())

	err := Phase31bMetricsServer(context.Background(), cl, st, nil, true)
	if err != nil {
		t.Fatalf("expected dry-run to succeed, got %v", err)
	}
}

func TestMetricsServerManifestEmbedded(t *testing.T) {
	data, err := manifests.FS.ReadFile("metrics-server/metrics-server.yaml")
	if err != nil {
		t.Fatalf("failed to read embedded metrics-server manifest: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("embedded metrics-server manifest is empty")
	}
}
