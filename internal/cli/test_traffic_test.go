package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/jumphost"
)

func resetTrafficFlags() {
	flagTrafficConfig = ""
	flagTrafficVIP = ""
	flagTrafficIterations = 5
	flagTrafficTimeout = 10 * time.Second
	flagTrafficInstanceID = ""
	flagTrafficRegion = ""
	flagTrafficUser = jumphost.DefaultSSHUser
	flagTrafficHost = ""
	flagTrafficDryRun = false
	flagOutput = ""
}

func withCWD(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	defer func() { _ = os.Chdir(orig) }()
	fn()
}

func writeTestTrafficClusterYAML(t *testing.T, dir string) string {
	t.Helper()
	keysDir := filepath.Join(dir, "keys")
	if err := os.MkdirAll(keysDir, 0o750); err != nil {
		t.Fatalf("mkdir keys: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keysDir, "far.tgz"), []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write far: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keysDir, "license.jwt"), []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write jwt: %v", err)
	}
	content := `apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: bnk-test
  subscriptionId: 00000000-0000-0000-0000-000000000000
  location: eastus
pattern: host-device
network:
  vnetCidr: 10.0.0.0/16
  subnets:
    system: 10.0.0.0/20
    external: 10.0.17.0/24
cluster:
  kubernetesVersion: "1.32.0"
  systemNodePool:
    name: system
    vmSize: Standard_D4s_v5
    count: 2
  tmmNodePool:
    name: tmm
    vmSize: Standard_D8s_v5
    count: 1
bnk:
  manifestVersion: "2.4.0-3.2882.0"
  farArchive: ./keys/far.tgz
  jwt: ./keys/license.jwt
`
	cfgPath := filepath.Join(dir, "cluster.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write cluster.yaml: %v", err)
	}
	return cfgPath
}

func TestTestTraffic_DryRunAndState(t *testing.T) {
	defer resetTrafficFlags()
	tmp := t.TempDir()
	withCWD(t, tmp, func() {
		cfgPath := writeTestTrafficClusterYAML(t, tmp)
		st, err := state.Load(filepath.Join(".azrbnkctl", "bnk-test"))
		if err != nil {
			t.Fatalf("state.Load: %v", err)
		}
		st.Set("JUMPHOST_INSTANCE_ID", "bnk-test-jumphost")
		st.Set("JUMPHOST_BNK_EXT_ENI_IP", "10.0.17.50")
		if err := st.Save(); err != nil {
			t.Fatalf("st.Save: %v", err)
		}

		flagTrafficConfig = cfgPath
		flagTrafficDryRun = true

		if err := runTestTraffic(testTrafficCmd, nil); err != nil {
			t.Fatalf("runTestTraffic dry-run failed: %v", err)
		}
	})
}

func TestTestTraffic_ProbesSuccessAndFailure(t *testing.T) {
	defer resetTrafficFlags()
	origFn := runCurlProbesFn
	defer func() { runCurlProbesFn = origFn }()

	var captured jumphost.ProbeOptions
	runCurlProbesFn = func(_ context.Context, opts jumphost.ProbeOptions) ([]jumphost.ProbeResult, error) {
		captured = opts
		return []jumphost.ProbeResult{
			{Iteration: 1, HTTPCode: 200, Seconds: 0.012},
			{Iteration: 2, HTTPCode: 200, Seconds: 0.010},
		}, nil
	}

	flagTrafficInstanceID = "vm-override"
	flagTrafficRegion = "westus2"
	flagTrafficVIP = "10.0.17.100"
	flagTrafficIterations = 2
	flagTrafficHost = "app.example.com"

	if err := runTestTraffic(testTrafficCmd, nil); err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if captured.EffectiveVMName() != "vm-override" || captured.VIP != "10.0.17.100" || captured.Hostname != "app.example.com" {
		t.Errorf("unexpected ProbeOptions: %+v", captured)
	}

	// Now simulate failure.
	runCurlProbesFn = func(_ context.Context, _ jumphost.ProbeOptions) ([]jumphost.ProbeResult, error) {
		return []jumphost.ProbeResult{
			{Iteration: 1, HTTPCode: 502, Seconds: 0.020, Err: "bad gateway"},
		}, nil
	}
	err := runTestTraffic(testTrafficCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "traffic probe failed") {
		t.Fatalf("expected traffic probe failed error, got: %v", err)
	}
}

func TestTestTraffic_MissingJumphostInState(t *testing.T) {
	defer resetTrafficFlags()
	t.Setenv("AZRBNKCTL_FORCE_SSH", "1")
	tmp := t.TempDir()
	withCWD(t, tmp, func() {
		cfgPath := writeTestTrafficClusterYAML(t, tmp)
		flagTrafficConfig = cfgPath
		flagTrafficDryRun = false
		err := runTestTraffic(testTrafficCmd, nil)
		if err == nil || !strings.Contains(err.Error(), "jumphost instance not found") {
			t.Fatalf("expected missing jumphost error, got: %v", err)
		}
	})
}
