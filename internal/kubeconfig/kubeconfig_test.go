package kubeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "azrbnkctl-kubeconfig-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	target := filepath.Join(tmpDir, "sub", "kubeconfig")
	content := []byte("apiVersion: v1\nkind: Config\n")

	if err := WriteFile(target, content); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	read, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(read) != string(content) {
		t.Fatalf("expected %q, got %q", string(content), string(read))
	}
}
