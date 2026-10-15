package phases

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	fmt.Fprintln(os.Stderr, "[phase 35] License CR applied OK")
	return nil
}

// purgeStaleCPCLSecrets deletes any stale licensestatus or cpcl-config-secret
// secrets in f5-utils and f5-cne-core before applying the License CR so CPCL
// does not get stuck in a stale state across re-runs.
func purgeStaleCPCLSecrets(ctx context.Context, client kubernetes.Interface) {
	if client == nil {
		return
	}
	for _, ns := range []string{"f5-utils", "f5-cne-core"} {
		for _, secretName := range []string{"licensestatus", "cpcl-config-secret"} {
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
