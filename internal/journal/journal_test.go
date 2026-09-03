package journal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestJournal_AppendAndList(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "azrbnkctl-journal-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	jPath := filepath.Join(tmpDir, "journal.jsonl")

	e1 := Entry{
		Action:  "UP",
		Cluster: "test-cluster",
		Message: "cluster provisioned",
	}
	if err := Append(jPath, e1); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	entries, err := List(jPath)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Action != "UP" {
		t.Errorf("expected UP action, got %s", entries[0].Action)
	}

	var buf bytes.Buffer
	RenderText(&buf, entries)
	if buf.Len() == 0 {
		t.Errorf("expected rendered text output")
	}
}
