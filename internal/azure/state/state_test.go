package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestState_SaveLoad(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load on empty dir failed: %v", err)
	}

	s.Set("TMM_EXT_IFACE", "eth1")
	s.Set("TMM_EXT_MAC", "00:0d:3a:12:34:56")
	s.Set("AKS_CLUSTER_ID", "/subscriptions/123/resourceGroups/rg/providers/Microsoft.ContainerService/managedClusters/aks")

	if err := s.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if got := loaded.Get("TMM_EXT_IFACE"); got != "eth1" {
		t.Errorf("Get(TMM_EXT_IFACE) = %q, want eth1", got)
	}
	if got := loaded.Get("TMM_EXT_MAC"); got != "00:0d:3a:12:34:56" {
		t.Errorf("Get(TMM_EXT_MAC) = %q, want 00:0d:3a:12:34:56", got)
	}
}

func TestState_ReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	s.MarkReadOnly()
	s.Set("KEY", "VALUE")
	if err := s.Save(); err != nil {
		t.Fatalf("Save in readOnly failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "state.env")); !os.IsNotExist(err) {
		t.Errorf("state.env file should not exist in readOnly mode")
	}
}
