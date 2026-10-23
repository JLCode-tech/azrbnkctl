package phases

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/manifests"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/render"
	"github.com/JLCode-tech/azrbnkctl/internal/manifest"
)

const floValuesTemplatePath = "shared/flo-values.yaml.tmpl"

// Phase32FLO installs the F5 Lifecycle Orchestrator (FLO) operator and CRDs.
// When the FAR archive is readable on disk, it pulls the f5-lifecycle-operator
// chart dynamically from oci://repo.f5.com/charts/f5-lifecycle-operator via the
// Helm v3 SDK and installs/upgrades it with rendered shared/flo-values.yaml.tmpl.
// Otherwise it falls back to the embedded flo/flo-manifest.yaml.
func Phase32FLO(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 32] flo: installing F5 Lifecycle Orchestrator")

	floVersion := cl.FLOVersion()
	if dryRun {
		fmt.Fprintf(os.Stderr, "[phase 32] [dry-run] would helm install/upgrade %s %s in %s\n",
			FLOReleaseName, floVersion, FLONamespace)
		if st != nil {
			st.Set("FLO_RELEASE_NAME", FLOReleaseName)
			st.Set("FLO_VERSION", floVersion)
			st.Set("FLO_NAMESPACE", FLONamespace)
		}
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase32: build REST config: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase32: creating applier: %w", err)
	}

	// Ensure cluster issuers (including <cluster>-ca-cluster-issuer) exist before
	// installing FLO so webhook and CWC certificates can issue immediately.
	_ = applyClusterIssuers(ctx, applier, cl)

	deployName := "flo-f5-lifecycle-operator"
	usedHelm := false

	if cl != nil && cl.Derived.FARArchiveFullPath != "" {
		if _, farKeyB64, authErr := manifest.ExtractFARAuth(cl.Derived.FARArchiveFullPath); authErr == nil && farKeyB64 != "" {
			if m, pErr := manifest.Probe(ctx, cl.Bnk.ManifestVersion, cl.Derived.FARArchiveFullPath); pErr == nil {
				if v, ok := m.HelmCharts["f5-lifecycle-operator"]; ok && v != "" {
					floVersion = v
				}
			}

			var jwtStr string
			if cl.Derived.JWTFullPath != "" {
				// #nosec G304 -- JWTFullPath is user-configured license path from cluster.yaml
				if jwtBytes, jErr := os.ReadFile(cl.Derived.JWTFullPath); jErr == nil {
					jwtStr = strings.TrimSpace(string(jwtBytes))
				}
			}

			valsTmpl, rErr := manifests.FS.ReadFile(floValuesTemplatePath)
			if rErr != nil {
				return fmt.Errorf("phase32: reading %s: %w", floValuesTemplatePath, rErr)
			}
			renderedVals, rErr := render.RenderFLOValues(valsTmpl, cl, jwtStr)
			if rErr != nil {
				return fmt.Errorf("phase32: rendering flo-values: %w", rErr)
			}
			var valuesMap map[string]interface{}
			if uErr := yaml.Unmarshal(renderedVals, &valuesMap); uErr != nil {
				return fmt.Errorf("phase32: parsing rendered flo-values: %w", uErr)
			}

			helmInst, hErr := NewHelmInstaller(st.KubeconfigPath(), farKeyB64)
			if hErr != nil {
				return fmt.Errorf("phase32: creating helm installer: %w", hErr)
			}
			if err := runFLOHelmInstall(ctx, helmInst, st, floVersion, valuesMap); err != nil {
				return err
			}
			deployName = FLOReleaseName
			usedHelm = true
		}
	}

	if !usedHelm {
		floBytes, err := manifests.FS.ReadFile("flo/flo-manifest.yaml")
		if err != nil {
			return fmt.Errorf("phase32: reading flo manifest: %w", err)
		}

		objs, err := k8s.ParseYAMLStream(floBytes)
		if err != nil {
			return fmt.Errorf("phase32: parsing flo manifest: %w", err)
		}

		if err := applier.ApplyAll(ctx, objs, true); err != nil {
			return fmt.Errorf("phase32: applying flo manifests: %w", err)
		}
	}

	dyn, err := k8s.NewDynamicFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase32: creating dynamic client: %w", err)
	}

	fmt.Fprintln(os.Stderr, "[phase 32] flo: waiting for CNEInstance CRD establishment...")
	if err := k8s.WaitForCRDEstablished(ctx, dyn, "cneinstances.k8s.f5.com", 3*time.Minute); err != nil {
		return fmt.Errorf("phase32: waiting for cneinstances CRD: %w", err)
	}

	cs, err := k8s.NewClientsetFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase32: creating clientset: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[phase 32] flo: waiting for %s Deployment readiness...\n", deployName)
	if err := k8s.WaitForDeploymentReady(ctx, cs, FLONamespace, deployName, 3*time.Minute); err != nil {
		return fmt.Errorf("phase32: waiting for %s deployment: %w", deployName, err)
	}

	fmt.Fprintln(os.Stderr, "[phase 32] FLO OK")
	return nil
}

func runFLOHelmInstall(_ context.Context, h HelmInstaller, st *state.State, floVersion string, valuesMap map[string]interface{}) error {
	fmt.Fprintf(os.Stderr, "[phase 32] flo: pulling chart %s@%s\n", FLOChartRef, floVersion)
	ch, err := h.PullAndLoad(FLOChartRef, floVersion)
	if err != nil {
		return fmt.Errorf("phase32: pulling FLO chart: %w", err)
	}

	releases, err := h.List(FLONamespace, "^"+FLOReleaseName+"$")
	if err != nil {
		return fmt.Errorf("phase32: listing helm releases: %w", err)
	}

	if len(releases) == 0 {
		fmt.Fprintf(os.Stderr, "[phase 32] flo: helm installing %s %s in %s\n", FLOReleaseName, floVersion, FLONamespace)
		if _, err := h.Install(FLOReleaseName, FLONamespace, ch, valuesMap); err != nil {
			return fmt.Errorf("phase32: helm install %s: %w", FLOReleaseName, err)
		}
	} else {
		existing := releases[0]
		deployedVersion := ""
		if existing.Chart != nil && existing.Chart.Metadata != nil {
			deployedVersion = existing.Chart.Metadata.Version
		}
		alreadyDeployed := existing.Info != nil && existing.Info.Status == release.StatusDeployed
		valuesUnchanged := k8s.HelmValuesEqual(existing.Config, valuesMap)
		if alreadyDeployed && deployedVersion == floVersion && valuesUnchanged {
			fmt.Fprintf(os.Stderr, "[phase 32] flo: release %s already at %s with unchanged values — skipping upgrade\n", FLOReleaseName, floVersion)
		} else {
			fmt.Fprintf(os.Stderr, "[phase 32] flo: helm upgrading %s (%q -> %q) in %s\n", FLOReleaseName, deployedVersion, floVersion, FLONamespace)
			if _, err := h.Upgrade(FLOReleaseName, FLONamespace, ch, valuesMap); err != nil {
				return fmt.Errorf("phase32: helm upgrade %s: %w", FLOReleaseName, err)
			}
		}
	}

	if st != nil {
		st.Set("FLO_RELEASE_NAME", FLOReleaseName)
		st.Set("FLO_VERSION", floVersion)
		st.Set("FLO_NAMESPACE", FLONamespace)
		st.Set("FLO_INSTALLED_AT", time.Now().UTC().Format(time.RFC3339))
		_ = st.Save()
	}
	return nil
}

func applyClusterIssuers(ctx context.Context, applier *k8s.Applier, cl *intent.Cluster) error {
	issuerBytes, err := manifests.FS.ReadFile("static/04-issuers.yaml")
	if err != nil {
		return fmt.Errorf("reading 04-issuers.yaml: %w", err)
	}
	objs, err := k8s.ParseYAMLStream(issuerBytes)
	if err != nil {
		return fmt.Errorf("parsing issuers: %w", err)
	}
	if cl != nil && cl.Metadata.Name != "" {
		clusterCAIssuer := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "cert-manager.io/v1",
				"kind":       "ClusterIssuer",
				"metadata": map[string]interface{}{
					"name": cl.Metadata.Name + "-ca-cluster-issuer",
				},
				"spec": map[string]interface{}{
					"ca": map[string]interface{}{
						"secretName": "ext-ca",
					},
				},
			},
		}
		objs = append(objs, clusterCAIssuer)
	}
	return applier.ApplyAll(ctx, objs, true)
}
