package manifest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
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

func TestParseReleaseManifest_ChartVersionsFormat(t *testing.T) {
	raw := []byte(`version: 2.3.1-0.0.14
chart-versions:
  - name: f5-lifecycle-operator
    version: v2.21.13-0.0.28
  - name: f5-tmm-controller
    version: v1.50.32-0.1.40
image-versions:
  - name: f5-tmm-env
    version: v11.7.0
`)
	m, err := ParseReleaseManifest(raw)
	if err != nil {
		t.Fatalf("ParseReleaseManifest failed: %v", err)
	}
	if got := m.HelmCharts["f5-lifecycle-operator"]; got != "v2.21.13-0.0.28" {
		t.Errorf("f5-lifecycle-operator = %q, want v2.21.13-0.0.28", got)
	}
	if got := m.DockerImgs["f5-tmm-env"]; got != "v11.7.0" {
		t.Errorf("f5-tmm-env = %q, want v11.7.0", got)
	}
}

func TestParseReleaseManifest_ReleasesFormat(t *testing.T) {
	raw := []byte(`releases:
  - version: 2.4.0-0.0.1234
    components:
      - name: f5-lifecycle-operator
        version: v2.30.0-0.5.2
        type: helm
        fullName: repo.f5.com/charts/f5-lifecycle-operator
      - name: f5-tmm-env
        version: v11.8.0-0.1.3
        type: container
        fullName: repo.f5.com/images/f5-tmm-env
`)
	m, err := ParseReleaseManifest(raw)
	if err != nil {
		t.Fatalf("ParseReleaseManifest failed: %v", err)
	}
	if got := m.HelmCharts["f5-lifecycle-operator"]; got != "v2.30.0-0.5.2" {
		t.Errorf("f5-lifecycle-operator = %q, want v2.30.0-0.5.2", got)
	}
	if got := m.DockerImgs["f5-tmm-env"]; got != "v11.8.0-0.1.3" {
		t.Errorf("f5-tmm-env = %q, want v11.8.0-0.1.3", got)
	}
}

func TestExtractFARAuth(t *testing.T) {
	tmp := t.TempDir()

	// 1. Raw base64 key
	rawPath := filepath.Join(tmp, "far-key.txt")
	if err := os.WriteFile(rawPath, []byte("  dGVzdC1rZXk= \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	user, pass, err := ExtractFARAuth(rawPath)
	if err != nil {
		t.Fatalf("ExtractFARAuth(raw) failed: %v", err)
	}
	if user != "_json_key_base64" || pass != "dGVzdC1rZXk=" {
		t.Errorf("got (%q, %q), want (_json_key_base64, dGVzdC1rZXk=)", user, pass)
	}

	// 2. dockerconfigjson inside a .tgz
	authB64 := base64.StdEncoding.EncodeToString([]byte("_json_key_base64:tgz-secret-key"))
	dockerCfg := `{"auths":{"repo.f5.com":{"auth":"` + authB64 + `"}}}`
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "config.json", Mode: 0o600, Size: int64(len(dockerCfg)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(dockerCfg)); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gw.Close()

	tgzPath := filepath.Join(tmp, "far.tgz")
	if err := os.WriteFile(tgzPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	user, pass, err = ExtractFARAuth(tgzPath)
	if err != nil {
		t.Fatalf("ExtractFARAuth(tgz) failed: %v", err)
	}
	if user != "_json_key_base64" || pass != "tgz-secret-key" {
		t.Errorf("got (%q, %q), want (_json_key_base64, tgz-secret-key)", user, pass)
	}
}
