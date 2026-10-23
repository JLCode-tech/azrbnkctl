package phases

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s/render"
)

// Phase35License applies the F5 BNK license JWT CR.
func Phase35License(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	var jwtToken string

	if cl.Derived.JWTFullPath != "" {
		jwtBytes, err := os.ReadFile(cl.Derived.JWTFullPath)
		if err != nil {
			jwtToken = strings.TrimSpace(cl.Bnk.JWT)
		} else {
			jwtToken = strings.TrimSpace(string(jwtBytes))
		}
	} else if cl.Bnk.JWT != "" {
		jwtToken = strings.TrimSpace(cl.Bnk.JWT)
	} else {
		fmt.Fprintln(os.Stderr, "[phase 35] license: no JWT provided (demo mode or unconfigured) — skipping")
		return nil
	}

	fmt.Fprintln(os.Stderr, "[phase 35] license: waiting for licenses.k8s.f5net.com CRD...")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 35] [dry-run] would apply License CR")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase35: build REST config: %w", err)
	}

	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase35: dynamic client: %w", err)
	}

	if err := k8s.WaitForCRDEstablished(ctx, dyn, "licenses.k8s.f5net.com", 5*time.Minute); err != nil {
		return fmt.Errorf("phase35: waiting for licenses.k8s.f5net.com CRD: %w", err)
	}
	fmt.Fprintln(os.Stderr, "[phase 35] licenses.k8s.f5net.com CRD established")

	if cs, csErr := kubernetes.NewForConfig(cfg); csErr == nil {
		purgeStaleCPCLSecrets(ctx, cs)
	}

	fmt.Fprintln(os.Stderr, "[phase 35] license: rendering and applying License CR")

	rendered, err := render.RenderLicenseCR(jwtToken)
	if err != nil {
		return fmt.Errorf("phase35: rendering license CR: %w", err)
	}

	applier, err := k8s.NewApplier(cfg)
	if err != nil {
		return fmt.Errorf("phase35: creating applier: %w", err)
	}

	objs, err := k8s.ParseYAMLStream(rendered)
	if err != nil {
		return fmt.Errorf("phase35: parsing license CR: %w", err)
	}

	if err := applier.ApplyAll(ctx, objs, true); err != nil {
		return fmt.Errorf("phase35: applying license CR: %w", err)
	}

	// Also apply License CR to f5-utils where f5-spk-cwc runs, ensuring CWC immediately detects and activates it.
	utilsLic := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "k8s.f5net.com/v1",
			"kind":       "License",
			"metadata": map[string]interface{}{
				"name":      "bnk-license",
				"namespace": "f5-utils",
			},
			"spec": map[string]interface{}{
				"operationMode": "connected",
				"jwt":           jwtToken,
			},
		},
	}
	if _, uErr := applier.Apply(ctx, utilsLic, true); uErr != nil {
		fmt.Fprintf(os.Stderr, "[phase 35] warn: applying License CR in f5-utils: %v\n", uErr)
	}

	fmt.Fprintln(os.Stderr, "[phase 35] License CR applied OK")
	return nil
}

// purgeStaleCPCLSecrets syncs Secret/cpcl-key-secret from ConfigMap/cpcl-key-cm
// and deletes stale CPCL/CWC state secrets in f5-utils, f5-cne-core, and f5-bnk
// before applying the License CR so CPCL does not get stuck in a stale state across re-runs.
func purgeStaleCPCLSecrets(ctx context.Context, client kubernetes.Interface) {
	if client == nil {
		return
	}

	var fallbackJWTKey string
	if cm, err := client.CoreV1().ConfigMaps("f5-utils").Get(ctx, "cpcl-key-cm", metav1.GetOptions{}); err == nil && cm != nil {
		fallbackJWTKey = cm.Data["jwt.key"]
	}

	for _, ns := range []string{"f5-utils", "f5-cne-core"} {
		jwtKey := fallbackJWTKey
		if cm, err := client.CoreV1().ConfigMaps(ns).Get(ctx, "cpcl-key-cm", metav1.GetOptions{}); err == nil && cm != nil && cm.Data["jwt.key"] != "" {
			jwtKey = cm.Data["jwt.key"]
		}
		if jwtKey == "" {
			continue
		}
		sec, err := client.CoreV1().Secrets(ns).Get(ctx, "cpcl-key-secret", metav1.GetOptions{})
		if err != nil || sec == nil {
			continue
		}
		if sec.Data == nil {
			sec.Data = make(map[string][]byte)
		}
		sec.Data["jwt.key"] = []byte(jwtKey)
		if _, updErr := client.CoreV1().Secrets(ns).Update(ctx, sec, metav1.UpdateOptions{}); updErr == nil {
			fmt.Fprintf(os.Stderr, "[phase 35] synced cpcl-key-secret from cpcl-key-cm in %s\n", ns)
		}
	}

	staleSecrets := []string{
		"context",
		"initialregistrationstatus",
		"licensestatus",
		"activationmessage",
		"activationstatus",
		"statehistory",
		"switchlicensestatus",
		"telemetrystatus",
		"telemetryreports",
		"f5-Flo-lic-msg",
		"f5-cwc-state",
		"cpcl-Dynamic-state",
		"cpcl-config-secret",
	}
	for _, ns := range []string{"f5-utils", "f5-cne-core", "f5-bnk"} {
		for _, secretName := range staleSecrets {
			err := client.CoreV1().Secrets(ns).Delete(ctx, secretName, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				continue
			}
			if err == nil {
				fmt.Fprintf(os.Stderr, "[phase 35] purged stale CPCL secret %s/%s\n", ns, secretName)
			}
		}
	}
}
