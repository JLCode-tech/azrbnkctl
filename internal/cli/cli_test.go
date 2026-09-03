package cli

import (
	"bytes"
	"os"
	"testing"
)

func TestRootCommand(t *testing.T) {
	cmd := RootCommand()
	if cmd == nil {
		t.Fatal("RootCommand returned nil")
	}

	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help failed: %v", err)
	}
}

func TestVersionCommand(t *testing.T) {
	cmd := RootCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("version failed: %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-cluster", "my-cluster"},
		{"My_Cluster@123", "my-cluster-123"},
		{"---", "bnk-aks"},
		{"", "bnk-aks"},
	}

	for _, tc := range tests {
		res := sanitizeName(tc.input)
		if res != tc.expected {
			t.Errorf("sanitizeName(%q) = %q, expected %q", tc.input, res, tc.expected)
		}
	}
}

func TestMain(m *testing.M) {
	os.Setenv("AZRBNKCTL_SKIP_AUTH", "1")
	os.Exit(m.Run())
}
