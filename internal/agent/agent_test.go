package agent

import (
	"os"
	"testing"
)

func TestAgent_Generate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "azrbnkctl-agent-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	origWd, _ := os.Getwd()
	_ = os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	files, err := Generate(HarnessAll, "", true)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if len(files) < 3 {
		t.Fatalf("expected at least 3 files, got %d", len(files))
	}

	for _, f := range files {
		if !fileExists(f) {
			t.Errorf("expected generated file %s to exist", f)
		}
	}
}
