package manifest

import (
	"context"
	"testing"
)

func TestProbe_Embedded(t *testing.T) {
	m, err := Probe(context.Background(), "", "")
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}

	if m.Version != DefaultManifestVersion {
		t.Fatalf("expected version %s, got %s", DefaultManifestVersion, m.Version)
	}

	if len(m.HelmCharts) == 0 {
		t.Fatal("expected helm charts, got 0")
	}

	if len(m.DockerImgs) == 0 {
		t.Fatal("expected docker images, got 0")
	}
}
