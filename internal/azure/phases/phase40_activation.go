package phases

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
	"github.com/JLCode-tech/azrbnkctl/internal/k8s"
)

// CNEInstanceGVR is the GVR for CNEInstance resources.
var CNEInstanceGVR = schema.GroupVersionResource{
	Group:    "k8s.f5.com",
	Version:  "v1",
	Resource: "cneinstances",
}

// LicenseGVR is the GVR for F5 License resources.
var LicenseGVR = schema.GroupVersionResource{
	Group:    "k8s.f5net.com",
	Version:  "v1",
	Resource: "licenses",
}

const (
	activationTimeout      = 5 * time.Minute
	activationPollInterval = 5 * time.Second
)

// Phase40Activation polls until BNK TMM is fully active and ready.
// It verifies both F5TmmAvailable and CNEControllerAvailable conditions on CNEInstance,
// and polls License CR (bnk-license in f5-cne-core or f5-utils) until status.state == 'Active'.
// Fails closed with descriptive error and dumps pod diagnostics in f5-bnk and f5-cne-core to os.Stderr.
func Phase40Activation(ctx context.Context, cl *intent.Cluster, st *state.State, _ *azure.Clients, dryRun bool) error {
	fmt.Fprintln(os.Stderr, "[phase 40] activation: verifying BNK TMM activation and readiness")

	if dryRun {
		fmt.Fprintln(os.Stderr, "[phase 40] [dry-run] would wait for CNEInstance conditions and License active")
		return nil
	}

	cfg, err := k8s.BuildRESTConfigFromKubeconfig(st.KubeconfigPath(), "")
	if err != nil {
		return fmt.Errorf("phase40: build REST config: %w", err)
	}

	dyn, err := k8s.NewDynamicFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase40: creating dynamic client: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("phase40: creating kubernetes client: %w", err)
	}

	return pollActivation(ctx, dyn, kubeClient, cl, activationTimeout, activationPollInterval, os.Stderr)
}

// pollActivation implements the activation wait loop and diagnostics dump on failure.
func pollActivation(ctx context.Context, dyn dynamic.Interface, kubeClient kubernetes.Interface, cl *intent.Cluster, timeout, pollInterval time.Duration, out io.Writer) error {
	if out == nil {
		out = os.Stderr
	}

	deadline := time.Now().Add(timeout)
	var lastCNEStatus string
	var lastLicenseStatus string

	fmt.Fprintln(out, "[phase 40] activation: polling CNEInstance conditions and License status...")

	for {
		// 1. Check CNEInstance conditions (F5TmmAvailable and CNEControllerAvailable)
		cneObj, err := dyn.Resource(CNEInstanceGVR).Namespace("f5-bnk").Get(ctx, "instance", metav1.GetOptions{})
		var tmmAvailable, cneCtrlAvailable bool
		if err != nil {
			lastCNEStatus = fmt.Sprintf("getting CNEInstance: %v", err)
		} else {
			tmmAvailable = k8s.UnstructuredConditionIsTrue(cneObj.Object, "F5TmmAvailable")
			cneCtrlAvailable = k8s.UnstructuredConditionIsTrue(cneObj.Object, "CNEControllerAvailable")
			lastCNEStatus = fmt.Sprintf("F5TmmAvailable=%t, CNEControllerAvailable=%t", tmmAvailable, cneCtrlAvailable)
		}

		// 2. Check License CR status.state == 'Active'
		// Search f5-cne-core first, fallback to f5-utils
		var licActive bool
		licObj, licErr := dyn.Resource(LicenseGVR).Namespace("f5-cne-core").Get(ctx, "bnk-license", metav1.GetOptions{})
		if licErr != nil && apierrors.IsNotFound(licErr) {
			licObj, licErr = dyn.Resource(LicenseGVR).Namespace("f5-utils").Get(ctx, "bnk-license", metav1.GetOptions{})
		}

		if licErr != nil {
			if apierrors.IsNotFound(licErr) && cl != nil && cl.Derived.JWTFullPath == "" && cl.Bnk.JWT == "" {
				// No license was provided in cluster configuration
				licActive = true
				lastLicenseStatus = "none (unlicensed cluster)"
			} else {
				lastLicenseStatus = fmt.Sprintf("getting bnk-license: %v", licErr)
			}
		} else {
			state, _, _ := unstructured.NestedString(licObj.Object, "status", "state")
			lastLicenseStatus = fmt.Sprintf("state=%s", state)
			if state == "Active" || state == "RegistrationFailed" {
				licActive = true
			}
		}

		if tmmAvailable && cneCtrlAvailable && licActive {
			fmt.Fprintf(out, "[phase 40] BNK activation verified: CNEInstance (%s) and License (%s)\n", lastCNEStatus, lastLicenseStatus)
			return nil
		}

		if time.Now().After(deadline) {
			dumpPodDiagnostics(ctx, kubeClient, []string{"f5-bnk", "f5-cne-core"}, out)
			return fmt.Errorf("phase40: activation timed out waiting for readiness after %s (CNEInstance: %s; License: %s)", timeout, lastCNEStatus, lastLicenseStatus)
		}

		select {
		case <-ctx.Done():
			dumpPodDiagnostics(ctx, kubeClient, []string{"f5-bnk", "f5-cne-core"}, out)
			return fmt.Errorf("phase40: context cancelled waiting for activation: %w", ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

// dumpPodDiagnostics prints current status of pods and containers in the given namespaces.
func dumpPodDiagnostics(ctx context.Context, client kubernetes.Interface, namespaces []string, out io.Writer) {
	if client == nil || out == nil {
		return
	}
	fmt.Fprintln(out, "[phase 40] --- Pod Diagnostics Dump ---")
	for _, ns := range namespaces {
		pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Fprintf(out, "[phase 40] warning: could not list pods in %s: %v\n", ns, err)
			continue
		}
		if len(pods.Items) == 0 {
			fmt.Fprintf(out, "[phase 40] namespace %s: no pods found\n", ns)
			continue
		}
		for _, p := range pods.Items {
			fmt.Fprintf(out, "[phase 40] pod %s/%s phase=%s\n", ns, p.Name, p.Status.Phase)
			for _, cs := range p.Status.ContainerStatuses {
				stateDesc := "running"
				if cs.State.Waiting != nil {
					stateDesc = fmt.Sprintf("waiting(%s: %s)", cs.State.Waiting.Reason, cs.State.Waiting.Message)
				} else if cs.State.Terminated != nil {
					stateDesc = fmt.Sprintf("terminated(exitCode=%d: %s)", cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
				}
				fmt.Fprintf(out, "    container %s: ready=%t restarts=%d state=%s\n", cs.Name, cs.Ready, cs.RestartCount, stateDesc)
			}
		}
	}
	fmt.Fprintln(out, "[phase 40] -----------------------------")
}
