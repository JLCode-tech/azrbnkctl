package phases

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
)

// Phase30Namespaces applies initial Kubernetes namespaces and the image pull secret.
func Phase30Namespaces(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 30] namespaces: creating f5-bnk, f5-utils, egress-demo and image pull secret")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 30] [dry-run] would apply namespaces and far-secret")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase30: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase30: creating applier: %w", err)
	}

	nsBytes, err := manifests.FS.ReadFile("static/01-namespaces.yaml")
	if err != nil {
		return fmt.Errorf("phase30: reading 01-namespaces.yaml: %w", err)
	}

	objs, err := k8s.ParseYAMLStream(nsBytes)
	if err != nil {
		return fmt.Errorf("phase30: parsing namespaces: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase30: applying namespaces: %w", err)
	}

	// Apply far-secret if provided
	if cl.Derived.FARArchiveFullPath != "" {
		dockerCfgJSON, err := extractAndBuildDockerConfig(cl.Derived.FARArchiveFullPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[phase 30] warn: extracting FAR archive: %v\n", err)
		} else {
			targetNamespaces := []string{"f5-bnk", "f5-utils", "default"}
			for _, ns := range targetNamespaces {
				secretObj := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata": map[string]interface{}{
							"name":      "far-secret",
							"namespace": ns,
						},
						"type": "kubernetes.io/dockerconfigjson",
						"data": map[string]interface{}{
							".dockerconfigjson": base64.StdEncoding.EncodeToString(dockerCfgJSON),
						},
					},
				}
				if _, err := applier.Apply(ctx, secretObj, true); err != nil {
					fmt.Fprintf(os.Stderr, "[phase 30] warn: applying far-secret to %s: %v\n", ns, err)
				} else {
					fmt.Fprintf(os.Stderr, "[phase 30] far-secret applied to %s\n", ns)
				}
			}
		}
	}

	_ = metav1.Now()
	fmt.Fprintln(os.Stderr, "[phase 30] namespaces OK")
	return nil
}

func extractAndBuildDockerConfig(farPath string) ([]byte, error) {
	data, err := os.ReadFile(farPath)
	if err != nil {
		return nil, fmt.Errorf("reading FAR file %s: %w", farPath, err)
	}

	// Check if gzip tar
	if len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(data))
		if err == nil {
			defer gz.Close()
			tr := tar.NewReader(gz)
			for {
				hdr, err := tr.Next()
				if err != nil {
					break
				}
				if hdr.Typeflag == tar.TypeReg {
					content, err := io.ReadAll(tr)
					if err == nil && len(content) > 0 {
						data = content
						break
					}
				}
			}
		}
	}

	// If already a dockerconfigjson format
	var testMap map[string]any
	if json.Unmarshal(data, &testMap) == nil {
		if _, hasAuths := testMap["auths"]; hasAuths {
			return data, nil
		}
	}

	// Otherwise treat as base64 key for repo.f5.com and GCP Artifact Registry
	farKeyB64 := strings.TrimSpace(string(data))
	authValueB64 := base64.StdEncoding.EncodeToString([]byte("_json_key_base64:" + farKeyB64))

	auths := map[string]any{
		"repo.f5.com":                                 map[string]any{"auth": authValueB64},
		"https://repo.f5.com":                         map[string]any{"auth": authValueB64},
		"us-west1-docker.pkg.dev":                     map[string]any{"auth": authValueB64},
		"https://us-west1-docker.pkg.dev":             map[string]any{"auth": authValueB64},
		"us-docker.pkg.dev":                           map[string]any{"auth": authValueB64},
		"https://us-docker.pkg.dev":                   map[string]any{"auth": authValueB64},
		"asia-southeast1-docker.pkg.dev":              map[string]any{"auth": authValueB64},
		"https://asia-southeast1-docker.pkg.dev":      map[string]any{"auth": authValueB64},
		"australia-southeast1-docker.pkg.dev":         map[string]any{"auth": authValueB64},
		"https://australia-southeast1-docker.pkg.dev": map[string]any{"auth": authValueB64},
		"docker.pkg.dev":                              map[string]any{"auth": authValueB64},
		"https://docker.pkg.dev":                      map[string]any{"auth": authValueB64},
	}

	rawJSONBytes, decErr := base64.StdEncoding.DecodeString(farKeyB64)
	if decErr == nil && len(rawJSONBytes) > 0 {
		authValueRaw := base64.StdEncoding.EncodeToString([]byte("_json_key:" + string(rawJSONBytes)))
		auths["_raw_json_key_us_west1"] = map[string]any{"auth": authValueRaw}
	}

	dockerCfg := map[string]any{
		"auths": auths,
	}
	return json.Marshal(dockerCfg)
}
