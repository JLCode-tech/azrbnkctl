package phases

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/JLCode-tech/azrbnkctl/internal/azure"
	"github.com/JLCode-tech/azrbnkctl/internal/azure/state"
	"github.com/JLCode-tech/azrbnkctl/internal/intent"
)

func fakeCNEInstance(tmmAvailable, ctrlAvailable bool) *unstructured.Unstructured {
	tmmStatus := "False"
	if tmmAvailable {
		tmmStatus = "True"
	}
	ctrlStatus := "False"
	if ctrlAvailable {
		ctrlStatus = "True"
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "k8s.f5.com/v1",
			"kind":       "CNEInstance",
			"metadata": map[string]any{
				"name":      "instance",
				"namespace": "f5-bnk",
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":   "F5TmmAvailable",
						"status": tmmStatus,
					},
					map[string]any{
						"type":   "CNEControllerAvailable",
						"status": ctrlStatus,
					},
				},
			},
		},
	}
}

func fakeLicense(ns, state string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "k8s.f5net.com/v1",
			"kind":       "License",
			"metadata": map[string]any{
				"name":      "bnk-license",
				"namespace": ns,
			},
			"status": map[string]any{
				"state": state,
			},
		},
	}
}

func fakeActivationDynamic(cne *unstructured.Unstructured, lic *unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k8s.f5.com", Version: "v1", Kind: "CNEInstance"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k8s.f5.com", Version: "v1", Kind: "CNEInstanceList"},
		&unstructured.UnstructuredList{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k8s.f5net.com", Version: "v1", Kind: "License"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "k8s.f5net.com", Version: "v1", Kind: "LicenseList"},
		&unstructured.UnstructuredList{},
	)

	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			CNEInstanceGVR: "CNEInstanceList",
			LicenseGVR:     "LicenseList",
		},
	)

	ctx := context.Background()
	if cne != nil {
		_, _ = dyn.Resource(CNEInstanceGVR).Namespace(cne.GetNamespace()).Create(ctx, cne, metav1.CreateOptions{})
	}
	if lic != nil {
		_, _ = dyn.Resource(LicenseGVR).Namespace(lic.GetNamespace()).Create(ctx, lic, metav1.CreateOptions{})
	}
	return dyn
}

func fakeKubeClientWithPods() *k8sfake.Clientset {
	podBNK := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "f5-tmm-0",
			Namespace: "f5-bnk",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "f5-tmm",
					Ready:        true,
					RestartCount: 0,
				},
			},
		},
	}
	podCNE := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cwc-controller-12345",
			Namespace: "f5-cne-core",
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "f5-spk-cwc",
					Ready:        true,
					RestartCount: 0,
				},
			},
		},
	}
	return k8sfake.NewSimpleClientset(podBNK, podCNE)
}

func TestPhase40Activation_Success(t *testing.T) {
	cne := fakeCNEInstance(true, true)
	lic := fakeLicense("f5-cne-core", "Active")
	dyn := fakeActivationDynamic(cne, lic)
	kube := fakeKubeClientWithPods()

	var buf bytes.Buffer
	cl := &intent.Cluster{
		Bnk: intent.BnkSpec{
			JWT: "some-jwt",
		},
	}

	err := pollActivation(context.Background(), dyn, kube, cl, 2*time.Second, 10*time.Millisecond, &buf)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if !strings.Contains(buf.String(), "BNK activation verified") {
		t.Errorf("expected verification log, got: %s", buf.String())
	}
}

func TestPhase40Activation_FallbackLicenseNamespace_Success(t *testing.T) {
	cne := fakeCNEInstance(true, true)
	lic := fakeLicense("f5-utils", "Active")
	dyn := fakeActivationDynamic(cne, lic)
	kube := fakeKubeClientWithPods()

	var buf bytes.Buffer
	cl := &intent.Cluster{
		Bnk: intent.BnkSpec{
			JWT: "some-jwt",
		},
	}

	err := pollActivation(context.Background(), dyn, kube, cl, 2*time.Second, 10*time.Millisecond, &buf)
	if err != nil {
		t.Fatalf("expected success with fallback license in f5-utils, got: %v", err)
	}
}

func TestPhase40Activation_Timeout_CNEInstanceNotReady(t *testing.T) {
	// CNEInstance not ready: F5TmmAvailable is false
	cne := fakeCNEInstance(false, true)
	lic := fakeLicense("f5-cne-core", "Active")
	dyn := fakeActivationDynamic(cne, lic)
	kube := fakeKubeClientWithPods()

	var buf bytes.Buffer
	cl := &intent.Cluster{
		Bnk: intent.BnkSpec{
			JWT: "some-jwt",
		},
	}

	err := pollActivation(context.Background(), dyn, kube, cl, 50*time.Millisecond, 10*time.Millisecond, &buf)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	if !strings.Contains(err.Error(), "activation timed out waiting for readiness") {
		t.Errorf("expected timeout message in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "F5TmmAvailable=false") {
		t.Errorf("expected F5TmmAvailable=false in error, got: %v", err)
	}

	// Verify pod diagnostics were dumped
	output := buf.String()
	if !strings.Contains(output, "Pod Diagnostics Dump") {
		t.Errorf("expected pod diagnostics dump in output, got: %s", output)
	}
	if !strings.Contains(output, "f5-tmm-0") {
		t.Errorf("expected pod name in diagnostics, got: %s", output)
	}
}

func TestPhase40Activation_Timeout_LicenseNotActive(t *testing.T) {
	// CNEInstance ready, but License is Pending
	cne := fakeCNEInstance(true, true)
	lic := fakeLicense("f5-cne-core", "Pending")
	dyn := fakeActivationDynamic(cne, lic)
	kube := fakeKubeClientWithPods()

	var buf bytes.Buffer
	cl := &intent.Cluster{
		Bnk: intent.BnkSpec{
			JWT: "some-jwt",
		},
	}

	err := pollActivation(context.Background(), dyn, kube, cl, 50*time.Millisecond, 10*time.Millisecond, &buf)
	if err == nil {
		t.Fatal("expected timeout error when license is Pending, got nil")
	}

	if !strings.Contains(err.Error(), "state=Pending") {
		t.Errorf("expected state=Pending in error message, got: %v", err)
	}
}

func TestPhase40Activation_DryRun(t *testing.T) {
	st, _ := state.New(t.TempDir())
	cl := &intent.Cluster{}
	clients := &azure.Clients{}

	err := Phase40Activation(context.Background(), cl, st, clients, true)
	if err != nil {
		t.Fatalf("expected dry-run to succeed, got: %v", err)
	}
}
