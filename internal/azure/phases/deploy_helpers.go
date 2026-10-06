package phases

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// DeployPollInterval paces WaitForDeployment and WaitForServiceAccount
// (a var so tests can shorten it).
var DeployPollInterval = 5 * time.Second

// WaitForDeployment returns the Deployment once it exists, polling until timeout.
// Used for FLO/CNEController-owned Deployments that appear some seconds after
// the CNEInstance CR is applied.
func WaitForDeployment(ctx context.Context, clients *Clients, ns, name string, timeout time.Duration) (*appsv1.Deployment, error) {
	deadline := time.Now().Add(timeout)
	for {
		d, err := clients.K8s.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			return d, nil
		}
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get deploy %s/%s: %w", ns, name, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("deploy %s/%s not created within %s", ns, name, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(DeployPollInterval):
		}
	}
}

// WaitForServiceAccount returns the ServiceAccount once it exists, polling
// until timeout. FLO creates the controller Deployment a few seconds before
// the SA it references (2 s apart on bnk-staging-test, 2026-09-11), so a
// caller that just saw the Deployment must not assume the SA is there yet.
func WaitForServiceAccount(ctx context.Context, clients *Clients, ns, name string, timeout time.Duration) (*corev1.ServiceAccount, error) {
	deadline := time.Now().Add(timeout)
	for {
		sa, err := clients.K8s.CoreV1().ServiceAccounts(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			return sa, nil
		}
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get sa %s/%s: %w", ns, name, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("sa %s/%s not created within %s", ns, name, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(DeployPollInterval):
		}
	}
}

// deploymentPodsHaveEnv reports whether every pod selected by deploy has a
// container exposing envName. With no pods there is nothing to fix, so it
// returns true.
func deploymentPodsHaveEnv(ctx context.Context, clients *Clients, deploy *appsv1.Deployment, envName string) (bool, error) {
	sel := labels.SelectorFromSet(deploy.Spec.Selector.MatchLabels).String()
	pods, err := clients.K8s.CoreV1().Pods(deploy.Namespace).List(ctx, metav1.ListOptions{LabelSelector: sel})
	if err != nil {
		return false, fmt.Errorf("list pods of deploy %s/%s: %w", deploy.Namespace, deploy.Name, err)
	}
	for _, p := range pods.Items {
		found := false
		for _, c := range p.Spec.Containers {
			for _, e := range c.Env {
				if e.Name == envName {
					found = true
				}
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

// restartDeployment triggers a rollout-restart by stamping the pod template,
// the same mechanism as `kubectl rollout restart`.
func restartDeployment(ctx context.Context, clients *Clients, ns, name string) error {
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"azrbnkctl.io/restartedAt":%q}}}}}`,
		time.Now().UTC().Format(time.RFC3339))
	if _, err := clients.K8s.AppsV1().Deployments(ns).Patch(ctx, name, types.StrategicMergePatchType,
		[]byte(patch), metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("restart deploy %s/%s: %w", ns, name, err)
	}
	return nil
}

// WaitForDeploymentAvailable returns once the Deployment exists and reports at
// least one available replica, polling until timeout. Phase 23b uses it so the
// F5 validating webhook (served by the cne-controller pod) has an endpoint
// before the first F5SPKVlan apply.
func WaitForDeploymentAvailable(ctx context.Context, clients *Clients, ns, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		d, err := clients.K8s.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("get deploy %s/%s: %w", ns, name, err)
		}
		if err == nil && d.Status.AvailableReplicas > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("deploy %s/%s not available within %s", ns, name, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(DeployPollInterval):
		}
	}
}

// IsWebhookUnavailable reports whether err is the apiserver telling us an
// admission webhook could not be reached — typically "no endpoints available
// for service" while the serving pod is still starting.
func IsWebhookUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "failed calling webhook") ||
		strings.Contains(msg, "no endpoints available for service")
}

// IsQuotaStatusUnknown reports the admission error the API server returns
// while a freshly created ResourceQuota has no computed usage yet
// ("status unknown for quota: f5-single-infra-quota"). FLO creates
// count/<crd> quotas next to the controller, and the quota controller needs a
// discovery cycle before it fills status.used; until then every create in
// that namespace is refused. Seen live 2026-09-14 on the Infra apply.
func IsQuotaStatusUnknown(err error) bool {
	return err != nil && strings.Contains(err.Error(), "status unknown for quota")
}

// IsTransientAdmission groups the admission failures that clear on their own.
func IsTransientAdmission(err error) bool {
	return IsWebhookUnavailable(err) || IsQuotaStatusUnknown(err)
}

// RetryWhileWebhookUnavailable runs fn, retrying on transient admission
// errors (webhook endpoint not ready, ResourceQuota usage not computed yet)
// until timeout. Any other error is returned immediately.
func RetryWhileWebhookUnavailable(ctx context.Context, timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil || !IsTransientAdmission(err) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("after %s admission still refuses the object: %w", timeout, err)
		}
		what := "admission webhook not ready yet"
		if IsQuotaStatusUnknown(err) {
			what = "ResourceQuota usage not computed yet"
		}
		fmt.Fprintf(os.Stderr, "[phase] %s, retrying in %s: %v\n", what, DeployPollInterval, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(DeployPollInterval):
		}
	}
}

// WaitForConditionTrue polls a namespaced CR until status.conditions carries
// condType with status "True", or timeout. The error names the last observed
// status/reason/message so a stuck Infra or Gateway is diagnosable from the log.
func WaitForConditionTrue(ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, ns, name, condType string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	last := "not found"
	for {
		obj, err := dyn.Resource(gvr).Namespace(ns).Get(ctx, name, metav1.GetOptions{})
		if err == nil {
			conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
			last = "no " + condType + " condition yet"
			for _, c := range conds {
				m, ok := c.(map[string]interface{})
				if !ok || m["type"] != condType {
					continue
				}
				if m["status"] == "True" {
					return nil
				}
				last = fmt.Sprintf("%s=%v reason=%v message=%v", condType, m["status"], m["reason"], m["message"])
			}
		} else if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get %s %s/%s: %w", gvr.Resource, ns, name, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s %s/%s: %s after %s", gvr.Resource, ns, name, last, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(DeployPollInterval):
		}
	}
}

// ensureTestNamespace ensures that the azrbnkctl test namespace exists.
func ensureTestNamespace(ctx context.Context, cs kubernetes.Interface) (bool, error) {
	_, err := cs.CoreV1().Namespaces().Get(ctx, K8sTestNamespace, metav1.GetOptions{})
	if err == nil {
		return false, nil
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: K8sTestNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "azrbnkctl",
			},
		},
	}
	_, err = cs.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return false, err
	}
	return true, nil
}

func checkAuthOrDie(_ *Clients) {}
