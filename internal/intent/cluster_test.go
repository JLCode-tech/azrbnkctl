package intent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBytes_Valid(t *testing.T) {
	yamlContent := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: test-cluster
  location: eastus
network:
  vnetCidr: 10.0.0.0/16
  subnets:
    system: 10.0.0.0/20
    public: 10.0.16.0/28
    external: 10.0.17.0/24
    internal: 10.0.18.0/24
pattern: dual-interface
cluster:
  kubernetesVersion: "1.32"
  tmmNodePool:
    vmSize: Standard_D8s_v5
    count: 2
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
`
	c, err := LoadBytes([]byte(yamlContent), "/tmp")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if c.Metadata.Name != "test-cluster" {
		t.Errorf("got name %q, want test-cluster", c.Metadata.Name)
	}
	if c.Derived.ResourceGroupName != "azrbnkctl-test-cluster-rg" {
		t.Errorf("got RG %q, want azrbnkctl-test-cluster-rg", c.Derived.ResourceGroupName)
	}
	if c.Derived.ExternalGatewayIP != "10.0.17.1" {
		t.Errorf("got external gw %q, want 10.0.17.1", c.Derived.ExternalGatewayIP)
	}
	if c.Derived.InternalGatewayIP != "10.0.18.1" {
		t.Errorf("got internal gw %q, want 10.0.18.1", c.Derived.InternalGatewayIP)
	}
}

func TestLoadBytes_VersionFloor(t *testing.T) {
	yamlContent := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: test-cluster
cluster:
  kubernetesVersion: "1.30"
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
`
	_, err := LoadBytes([]byte(yamlContent), "/tmp")
	if err == nil {
		t.Fatal("expected error for K8s version below floor 1.32, got nil")
	}
	if !strings.Contains(err.Error(), "mandated floor") {
		t.Errorf("expected error mentioning mandated floor, got: %v", err)
	}
}

func TestLoadBytes_UnknownFields(t *testing.T) {
	yamlContent := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: test-cluster
unknownField: true
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
`
	_, err := LoadBytes([]byte(yamlContent), "/tmp")
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestLoad_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "cluster.yaml")
	yamlContent := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: file-cluster
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
`
	if err := os.WriteFile(filePath, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	c, err := Load(filePath)
	if err != nil {
		t.Fatalf("unexpected Load error: %v", err)
	}

	if c.Derived.FARArchiveFullPath != filepath.Join(tmpDir, "cne_pull_64.json") {
		t.Errorf("got FAR path %q, want %q", c.Derived.FARArchiveFullPath, filepath.Join(tmpDir, "cne_pull_64.json"))
	}
}

func TestLoadBytes_TmmMtuIntAndString(t *testing.T) {
	intYAML := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: mtu-int-cluster
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
  tmmMtu: 1460
`
	cInt, err := LoadBytes([]byte(intYAML), "/tmp")
	if err != nil {
		t.Fatalf("unexpected error loading int tmmMtu: %v", err)
	}
	if cInt.Bnk.TmmMtu != 1460 {
		t.Errorf("got TmmMtu %d, want 1460", cInt.Bnk.TmmMtu)
	}

	strYAML := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: mtu-str-cluster
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
  tmmMtu: "9000"
`
	cStr, err := LoadBytes([]byte(strYAML), "/tmp")
	if err != nil {
		t.Fatalf("unexpected error loading string tmmMtu: %v", err)
	}
	if cStr.Bnk.TmmMtu != 9000 {
		t.Errorf("got TmmMtu %d, want 9000", cStr.Bnk.TmmMtu)
	}

	badYAML := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: mtu-bad-cluster
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
  tmmMtu: "not-a-number"
`
	if _, err := LoadBytes([]byte(badYAML), "/tmp"); err == nil {
		t.Fatal("expected error for non-numeric tmmMtu string, got nil")
	}

	unknownBnkYAML := `
apiVersion: azrbnkctl/v1
kind: Cluster
metadata:
  name: mtu-unk-cluster
bnk:
  farArchive: ./cne_pull_64.json
  jwt: ./license.jwt
  bogusField: true
`
	if _, err := LoadBytes([]byte(unknownBnkYAML), "/tmp"); err == nil {
		t.Fatal("expected error for unknown field in bnk block, got nil")
	}
}

func TestBnkIs24AndFLOVersion(t *testing.T) {
	c := &Cluster{Bnk: BnkSpec{ManifestVersion: "2.4.0"}}
	if !c.Bnk.Is24() {
		t.Error("expected 2.4.0 to be Is24() == true")
	}
	if got := c.FLOVersion(); got != "v2.30.0-0.5.2" {
		t.Errorf("got FLOVersion %q, want v2.30.0-0.5.2", got)
	}

	c23 := &Cluster{Bnk: BnkSpec{ManifestVersion: "2.3.3-3.2598.3-0.0.509"}}
	if c23.Bnk.Is24() {
		t.Error("expected 2.3.3 to be Is24() == false")
	}
	if got := c23.FLOVersion(); got != "v2.21.13-0.0.64" {
		t.Errorf("got FLOVersion %q, want v2.21.13-0.0.64", got)
	}
}
